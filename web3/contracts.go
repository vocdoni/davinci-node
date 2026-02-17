package web3

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethapitypes "github.com/ethereum/go-ethereum/signer/core/apitypes"

	npbindings "github.com/vocdoni/davinci-contracts/golang-types"
	vbindings "github.com/vocdoni/davinci-contracts/golang-types/verifiers"
	"github.com/vocdoni/davinci-node/config"
	ethSigner "github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3/rpc"
	"github.com/vocdoni/davinci-node/web3/txmanager"
)

const (
	// web3QueryTimeout is the timeout for web3 queries.
	web3QueryTimeout = 10 * time.Second

	// web3WaitTimeout is the timeout for waiting for web3 transactions to be sent.
	web3WaitTimeout = 2 * time.Minute

	// maxPastBlocksToWatch is the maximum number of past blocks to watch for events.
	maxPastBlocksToWatch = 9990

	// currentBlockIntervalUpdate is the interval to update the current block.
	currentBlockIntervalUpdate = 5 * time.Second
)

var (
	organizationRegistryABI      *abi.ABI
	processRegistryABI           *abi.ABI
	stateTransitionZKVerifierABI *abi.ABI
	resultsZKVerifierABI         *abi.ABI
	censusValidatorABI           *abi.ABI
)

func init() {
	parseABI := func(raw string) *abi.ABI {
		parsedABI, err := abi.JSON(strings.NewReader(raw))
		if err != nil {
			panic(fmt.Errorf("failed to parse ABI: %w", err))
		}
		return &parsedABI
	}
	organizationRegistryABI = parseABI(npbindings.OrganizationRegistryMetaData.ABI)
	processRegistryABI = parseABI(npbindings.ProcessRegistryMetaData.ABI)
	stateTransitionZKVerifierABI = parseABI(vbindings.StateTransitionVerifierGroth16MetaData.ABI)
	resultsZKVerifierABI = parseABI(vbindings.ResultsVerifierGroth16MetaData.ABI)
	censusValidatorABI = parseABI(npbindings.ICensusValidatorMetaData.ABI)
}

// Addresses contains the addresses of the contracts deployed in the network.
type Addresses struct {
	OrganizationRegistry      common.Address
	ProcessRegistry           common.Address
	StateTransitionZKVerifier common.Address
	ResultsZKVerifier         common.Address
}

// ContractABIs contains the ABIs of the deployed contracts.
type ContractABIs struct {
	OrganizationRegistry      *abi.ABI
	ProcessRegistry           *abi.ABI
	StateTransitionZKVerifier *abi.ABI
	ResultsZKVerifier         *abi.ABI
	CensusValidator           *abi.ABI
}

// Contracts contains the bindings to the deployed contracts.
type Contracts struct {
	ChainID                  uint64
	ContractsAddresses       *Addresses
	ContractABIs             *ContractABIs
	Web3ConsensusAPIEndpoint string
	GasMultiplier            float64
	organizations            *npbindings.OrganizationRegistry
	processes                *npbindings.ProcessRegistry
	web3pool                 *rpc.Web3Pool
	cli                      *rpc.Client
	signer                   *ethSigner.Signer

	currentBlock           uint64
	currentBlockLastUpdate time.Time
	currentBlockMutex      sync.Mutex

	knownProcesses                map[types.ProcessID]struct{}
	knownProcessesMutex           sync.RWMutex
	lastWatchProcessCreationBlock uint64
	lastWatchProcessChangesBlock  uint64
	watchBlockMutex               sync.RWMutex
	knownOrganizations            map[string]struct{}
	lastWatchOrgBlock             uint64

	// Transaction manager for nonce management and stuck transaction recovery
	txManager *txmanager.TxManager
	// Whether the current contracts support blob transactions
	supportForBlobTxs bool
}

// New creates a new Contracts instance with the given web3 endpoints.
// It initializes the web3 pool and the client, and sets up the known processes
func New(web3rpcs []string, web3cApi string, gasMultiplier float64) (*Contracts, error) {
	w3pool := rpc.NewWeb3Pool()
	var chainID *uint64
	for _, rpc := range web3rpcs {
		cID, err := w3pool.AddEndpoint(rpc)
		if err != nil {
			log.Warnw("skipping web3 endpoint", "rpc", rpc, "error", err)
			continue
		}
		if chainID == nil {
			chainID = &cID
		}
		if *chainID != cID {
			return nil, fmt.Errorf("web3 endpoints have different chain IDs: %d and %d", *chainID, cID)
		}
	}
	if chainID == nil {
		return nil, fmt.Errorf("no web3 endpoints provided")
	}
	cli, err := w3pool.Client(*chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// get the last block number
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()
	lastBlock, err := cli.BlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get block number: %w", err)
	}

	log.Infow("web3 client initialized",
		"chainID", *chainID,
		"consensusAPI", web3cApi,
		"lastBlock", lastBlock,
		"numEndpoints", len(web3rpcs),
		"numBlocksToWatch", maxPastBlocksToWatch,
	)

	if web3cApi == "" {
		log.Warn("no consensus API endpoint provided, sync remote state will be disabled!")
	}

	// calculate the start block to watch
	startBlock := max(int64(lastBlock)-maxPastBlocksToWatch, 0)

	// Default to 1.0 if not set or invalid
	if gasMultiplier <= 0 {
		gasMultiplier = 1.0
	}

	return &Contracts{
		ChainID:                       *chainID,
		web3pool:                      w3pool,
		cli:                           cli,
		Web3ConsensusAPIEndpoint:      web3cApi,
		GasMultiplier:                 gasMultiplier,
		knownProcesses:                make(map[types.ProcessID]struct{}),
		knownOrganizations:            make(map[string]struct{}),
		lastWatchProcessCreationBlock: uint64(startBlock),
		lastWatchProcessChangesBlock:  uint64(startBlock),
		lastWatchOrgBlock:             uint64(startBlock),
		currentBlock:                  lastBlock,
		currentBlockLastUpdate:        time.Now(),
	}, nil
}

// Web3Pool returns the web3 pool used by the Contracts instance.
func (c *Contracts) Web3Pool() *rpc.Web3Pool {
	return c.web3pool
}

// Client returns the web3 client used by the Contracts instance.
func (c *Contracts) Client() *rpc.Client {
	return c.cli
}

// Signer returns the signer used by the Contracts instance.
func (c *Contracts) Signer() *ethSigner.Signer {
	return c.signer
}

// SetTxManager sets the transaction manager to be used by the Contracts
// instance.
func (c *Contracts) SetTxManager(tm *txmanager.TxManager) {
	c.txManager = tm
}

// CurrentBlock returns the current block number for the chain.
func (c *Contracts) CurrentBlock() uint64 {
	c.currentBlockMutex.Lock()
	defer c.currentBlockMutex.Unlock()
	now := time.Now()
	if c.currentBlockLastUpdate.Add(currentBlockIntervalUpdate).Before(now) {
		ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
		defer cancel()
		block, err := c.cli.BlockNumber(ctx)
		if err != nil {
			log.Warnw("failed to get block number", "error", err)
			return c.currentBlock
		}
		c.currentBlock = block
		c.currentBlockLastUpdate = now
	}
	return c.currentBlock
}

// SupportBlobTxs returns whether the current contracts support blob
// transactions.
func (c *Contracts) SupportBlobTxs() bool {
	return c.supportForBlobTxs
}

// supportBlobTxs queries the ProcessRegistry contract to check if blob
// transactions are supported.
func (c *Contracts) supportBlobTxs() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()
	supported, err := c.processes.BlobsDA(&bind.CallOpts{
		Context: ctx,
	})
	if err != nil {
		return false, fmt.Errorf("failed to check blob support: %w", err)
	}
	return supported, nil
}

// LoadContracts loads the contracts
func (c *Contracts) LoadContracts(addresses *Addresses) error {
	if addresses == nil {
		addresses = &Addresses{}
	}

	if addresses.OrganizationRegistry == (common.Address{}) {
		organizationRegistryAddr, err := c.OrganizationRegistryAddress()
		if err != nil {
			return fmt.Errorf("failed to get organization registry address: %w", err)
		}
		addresses.OrganizationRegistry = common.HexToAddress(organizationRegistryAddr)
	}

	if addresses.ProcessRegistry == (common.Address{}) {
		processRegistryAddr, err := c.ProcessRegistryAddress()
		if err != nil {
			return fmt.Errorf("failed to get process registry address: %w", err)
		}
		addresses.ProcessRegistry = common.HexToAddress(processRegistryAddr)
	}
	log.Debugw("loading contracts",
		"chainID", c.ChainID,
		"organizationRegistry", addresses.OrganizationRegistry.Hex(),
		"processRegistry", addresses.ProcessRegistry.Hex())

	organizations, err := npbindings.NewOrganizationRegistry(addresses.OrganizationRegistry, c.cli)
	if err != nil {
		return fmt.Errorf("failed to bind organization registry: %w", err)
	}
	process, err := npbindings.NewProcessRegistry(addresses.ProcessRegistry, c.cli)
	if err != nil {
		return fmt.Errorf("failed to bind process registry: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()

	// checking that the state transition proving key on the sequencer is compatible with the state transition verification key on the smart contract.
	stkey, err := process.GetSTVerifierVKeyHash(&bind.CallOpts{Context: ctx})
	if err != nil {
		return fmt.Errorf("failed to get state transition verifier address: %w", err)
	}
	if !bytes.Equal(stkey[:], types.HexStringToHexBytesMustUnmarshal(config.StateTransitionProvingKeyHash)) {
		return fmt.Errorf("proving key hash mismatch with the one provided by the smart contract: %s != %x", config.StateTransitionProvingKeyHash, stkey)
	}

	// checking that the results proving key on the sequencer is compatible with the results verification key on the smart contract.
	rkey, err := process.GetRVerifierVKeyHash(&bind.CallOpts{Context: ctx})
	if err != nil {
		return fmt.Errorf("failed to get results verifier address: %w", err)
	}
	if !bytes.Equal(rkey[:], types.HexStringToHexBytesMustUnmarshal(config.ResultsVerifierProvingKeyHash)) {
		return fmt.Errorf("proving key hash mismatch with the one provided by the smart contract: %s != %x", config.StateTransitionProvingKeyHash, rkey)
	}

	c.ContractsAddresses = addresses
	c.processes = process
	c.organizations = organizations

	c.ContractABIs = &ContractABIs{
		OrganizationRegistry:      organizationRegistryABI,
		ProcessRegistry:           processRegistryABI,
		StateTransitionZKVerifier: stateTransitionZKVerifierABI,
		ResultsZKVerifier:         resultsZKVerifierABI,
		CensusValidator:           censusValidatorABI,
	}

	// check for blob transaction support querying the ProcessRegistry contract
	c.supportForBlobTxs, err = c.supportBlobTxs()
	if err != nil {
		log.Warnw("failed to check blob transaction support, defaulting to false", "error", err)
	}
	return nil
}

// CheckTxStatus checks the status of a transaction given its hash.
// Returns true if the transaction was successful, false otherwise.
func (c *Contracts) CheckTxStatus(txHash common.Hash) (bool, error) {
	ethcli, err := c.cli.EthClient()
	if err != nil {
		return false, fmt.Errorf("failed to get eth client: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()
	receipt, err := ethcli.TransactionReceipt(ctx, txHash)
	if err != nil {
		return false, fmt.Errorf("failed to get transaction receipt: %w", err)
	}
	return receipt.Status == 1, nil
}

// WaitTxByHash waits for a transaction to be mined given its hash. If the
// transaction is not mined within the timeout, it returns an error.
func (c *Contracts) WaitTxByHash(txHash common.Hash, timeOut time.Duration, cb ...func(error)) error {
	if c.txManager != nil {
		return c.txManager.WaitTxByHash(txHash, timeOut, cb...)
	}
	return c.waitTx(txHash, timeOut)
}

// WaitTxByID waits for a transaction to be mined given its hash. If the
// transaction is not mined within the timeout, it returns an error.
func (c *Contracts) WaitTxByID(id []byte, timeOut time.Duration, cb ...func(error)) error {
	if c.txManager == nil {
		return fmt.Errorf("no transaction manager configured")
	}
	return c.txManager.WaitTxByID(id, timeOut, cb...)
}

// waitTx waits for a transaction to be mined given its hash.
func (c *Contracts) waitTx(txHash common.Hash, timeOut time.Duration) error {
	timeout := time.After(timeOut)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for tx %s", txHash.Hex())
		case <-ticker.C:
			// Check if the transaction is mined
			if status, _ := c.CheckTxStatus(txHash); status {
				return nil
			}
		}
	}
}

// AddWeb3Endpoint adds a new web3 endpoint to the pool.
func (c *Contracts) AddWeb3Endpoint(web3rpc string) error {
	_, err := c.web3pool.AddEndpoint(web3rpc)
	return err
}

// SetAccountPrivateKey sets the private key to be used for signing transactions.
func (c *Contracts) SetAccountPrivateKey(hexPrivKey string) error {
	signer, err := ethSigner.NewSignerFromHex(hexPrivKey)
	if err != nil {
		return fmt.Errorf("failed to add private key: %w", err)
	}
	c.signer = signer
	return nil
}

// AccountAddress returns the address of the account used to sign transactions.
func (c *Contracts) AccountAddress() common.Address {
	return c.signer.Address()
}

// SignMessage signs a message with the account private key.
func (c *Contracts) SignMessage(msg []byte) ([]byte, error) {
	if c.signer == nil {
		return nil, fmt.Errorf("no private key set")
	}
	signature, err := c.signer.Sign(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to sign message: %w", err)
	}
	return signature.Bytes(), nil
}

// AccountNonce returns the nonce of the account used to sign transactions.
func (c *Contracts) AccountNonce() (uint64, error) {
	if c.signer == nil {
		return 0, fmt.Errorf("no private key set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()
	return c.cli.PendingNonceAt(ctx, c.signer.Address())
}

// authTransactOpts helper method creates the transact options with the private
// key configured in the CommunityHub. It sets the nonce, gas price, and gas
// limit. If something goes wrong creating the signer, getting the nonce, or
// getting the gas price, it returns an error.
func (c *Contracts) authTransactOpts() (*bind.TransactOpts, error) {
	if c.signer == nil {
		return nil, fmt.Errorf("no private key set")
	}
	bChainID := new(big.Int).SetUint64(c.ChainID)
	auth := bind.NewKeyedTransactor((*ecdsa.PrivateKey)(c.signer), bChainID)

	// create the context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// set the nonce
	nonce, err := c.cli.PendingNonceAt(ctx, c.signer.Address())
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce: %w", err)
	}
	auth.Nonce = new(big.Int).SetUint64(nonce)
	return auth, nil
}

// SimulateContractCall simulates a contract call using the eth_simulateV1 RPC
// method. If blobsSidecar is provided, it will simulate an EIP4844 transaction
// with blob data. If blobsSidecar is nil, it will simulate a regular contract
// call.
// NOTE: this is a temporary method to simulate contract calls it works on geth
// but not expected to work on other clients or external rpc providers.
func (c *Contracts) SimulateContractCall(
	ctx context.Context,
	contractAddr common.Address,
	data []byte,
	blobsSidecar *types.BlobTxSidecar,
) error {
	if (contractAddr == common.Address{}) {
		return fmt.Errorf("empty contract address")
	}
	if c.signer == nil {
		return fmt.Errorf("no signer defined")
	}

	auth, err := c.authTransactOpts()
	if err != nil {
		return fmt.Errorf("failed to create transactor: %w", err)
	}

	tipCap, err := c.cli.SuggestGasTipCap(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tip cap: %w", err)
	}
	baseFee, err := c.cli.SuggestGasPrice(ctx)
	if err != nil {
		return fmt.Errorf("failed to get base fee: %w", err)
	}
	// Cap gas fee (baseFee * 2 + tipCap)
	gasFeeCap := new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(2)), tipCap)

	callMsg := ethereum.CallMsg{
		From: c.signer.Address(),
		To:   &contractAddr,
		Data: data,
	}
	if blobsSidecar != nil {
		callMsg.BlobHashes = blobsSidecar.BlobHashes()
	}

	gas, err := txmanager.EstimateGas(ctx, c.cli, c.txManager, callMsg, txmanager.DefaultGasEstimateOpts, txmanager.DefaultCancelGasFallback)
	if err != nil {
		if reason, ok := c.DecodeError(err); ok {
			return fmt.Errorf("failed to estimate gas: %w (decoded: %s)", err, reason)
		}
		return fmt.Errorf("failed to estimate gas: %w", err)
	}

	call := gethapitypes.SendTxArgs{
		From:                 common.NewMixedcaseAddress(c.signer.Address()),
		To:                   ptr(common.NewMixedcaseAddress(contractAddr)),
		Data:                 ptr(hexutil.Bytes(data)),
		MaxPriorityFeePerGas: (*hexutil.Big)(tipCap),
		MaxFeePerGas:         (*hexutil.Big)(gasFeeCap),
		Nonce:                hexutil.Uint64(auth.Nonce.Uint64()),
		Gas:                  hexutil.Uint64(gas),
	}

	if blobsSidecar != nil {
		// Base fee for *blob gas* (separate market). Use RPC eth_blobBaseFee.
		blobBaseFee, err := c.cli.BlobBaseFee(ctx)
		if err != nil {
			return fmt.Errorf("blob base fee: %w", err)
		}
		// Apply gas multiplier: (blobBaseFee * 2) * multiplier
		baseBlobFeeCap := new(big.Int).Mul(blobBaseFee, big.NewInt(2))
		blobFeeCap := applyGasMultiplier(baseBlobFeeCap, c.GasMultiplier)
		call.BlobFeeCap = (*hexutil.Big)(blobFeeCap)

		sidecar := blobsSidecar.AsGethSidecar()
		call.BlobHashes = sidecar.BlobHashes()
		call.Blobs = sidecar.Blobs
		call.Commitments = sidecar.Commitments
		call.Proofs = sidecar.Proofs
	}

	simReq := SimulationRequest{
		BlockStateCalls: []BlockStateCall{
			{
				Calls: []gethapitypes.SendTxArgs{call},
			},
		},
		Validation:             true,
		ReturnFullTransactions: true,
	}

	var simBlocks []SimulatedBlock
	if err := c.cli.CallSimulation(ctx, &simBlocks, simReq, "latest"); err != nil {
		return fmt.Errorf("eth_simulateV1 RPC error: %w", err)
	}

	if len(simBlocks) == 0 || len(simBlocks[0].Calls) == 0 {
		return fmt.Errorf("no simulation result")
	}

	callResult := simBlocks[0].Calls[0]
	if callResult.Status == "0x1" {
		// success, nothing to do
		return nil
	}

	// eth_simulateV1 returns data as ReturnData rather than inside Error,
	// so we need to fill Error.Data before calling DecodeError
	if callResult.Error != nil && len(callResult.Error.Data) == 0 && len(callResult.ReturnData) > 0 {
		callResult.Error.Data = callResult.ReturnData
	}
	if reason, ok := c.DecodeError(callResult.Error); ok {
		return fmt.Errorf("call reverted: %w (decoded: %s)", callResult.Error, reason)
	}
	return fmt.Errorf("call reverted: %w", callResult.Error)
}

// DecodeError tries to decode revert reasons or custom errors from err,
// and returns true if successful.
func (c *Contracts) DecodeError(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	rpcErr := rpc.ParseError(err)
	if rpcErr == nil || len(rpcErr.Data) < 4 {
		return "", false
	}

	errId := [4]byte{}
	copy(errId[:], rpcErr.Data[:4])

	// 1) Try custom errors from all loaded ABIs
	var decoded string
	abiErr := c.ContractABIs.forEachABI(func(abiName string, a *abi.ABI) error {
		if abiErr, err := a.ErrorByID(errId); err == nil {
			// unpack args if any
			vals, err := abiErr.Inputs.Unpack(rpcErr.Data[4:])
			if err != nil || len(vals) == 0 {
				decoded = fmt.Sprintf("%s %s = %s", abiName, rpcErr.Data[:4].String(), abiErr.String())
				return nil
			}
			decoded = fmt.Sprintf("%s %s = %s %+v", abiName, rpcErr.Data[:4].String(), abiErr.Name, vals)
			return nil
		}
		return nil
	})
	if abiErr != nil {
		log.Warnf("forEachABI failed with err: %s", abiErr)
	}
	if decoded != "" {
		return decoded, true
	}

	// 2) Fallback to standard Error(string)/Panic(uint256)
	decoded, uerr := abi.UnpackRevert(rpcErr.Data)
	if uerr != nil {
		log.Warnf("abi.UnpackRevert failed with err: %s", uerr)
		return "", false
	}

	return decoded, true
}

// forEachABI calls fn(name, abi) for each non-nil *abi.ABI field.
// Stops and returns an error if fn returns an error.
func (c *ContractABIs) forEachABI(fn func(fieldName string, a *abi.ABI) error) error {
	if c == nil {
		return fmt.Errorf("no contract ABIs")
	}
	v := reflect.ValueOf(c).Elem() // reflect.Value of the struct
	t := v.Type()                  // reflect.Type of the struct
	for i := range v.NumField() {  // loop fields
		fieldVal := v.Field(i)
		if fieldVal.IsNil() {
			continue
		}
		abiPtr, ok := fieldVal.Interface().(*abi.ABI)
		if !ok {
			// should never happen
			continue
		}
		fieldName := t.Field(i).Name
		if err := fn(fieldName, abiPtr); err != nil {
			return fmt.Errorf("%s: %w", fieldName, err)
		}
	}
	return nil
}

// ProcessRegistryABI returns the ABI of the ProcessRegistry contract.
func (c *Contracts) ProcessRegistryABI() *abi.ABI { return processRegistryABI }

// ResultsRegistryABI returns the ABI of the ResultsRegistry contract.
func (c *Contracts) OrganizationRegistryABI() *abi.ABI { return organizationRegistryABI }

// StateTransitionVerifierABI returns the ABI of the ZKVerifier contract.
func (c *Contracts) StateTransitionVerifierABI() *abi.ABI { return stateTransitionZKVerifierABI }

// ResultsVerifierABI returns the ABI of the ResultsVerifier contract.
func (c *Contracts) ResultsVerifierABI() *abi.ABI { return resultsZKVerifierABI }

// CensusValidatorABI returns the ABI of the CensusValidator contract.
func (c *Contracts) CensusValidatorABI() *abi.ABI { return censusValidatorABI }

func (c *Contracts) ProcessRegistryAddress() (string, error) {
	chainName, ok := npbindings.AvailableNetworksByID[uint32(c.ChainID)]
	if !ok {
		return "", fmt.Errorf("unknown chain ID %d", c.ChainID)
	}
	return npbindings.GetContractAddress(npbindings.ProcessRegistryContract, chainName), nil
}

func (c *Contracts) StateTransitionVerifierAddress() (string, error) {
	chainName, ok := npbindings.AvailableNetworksByID[uint32(c.ChainID)]
	if !ok {
		return "", fmt.Errorf("unknown chain ID %d", c.ChainID)
	}
	return npbindings.GetContractAddress(npbindings.StateTransitionVerifierGroth16Contract, chainName), nil
}

func (c *Contracts) ResultsVerifierAddress() (string, error) {
	chainName, ok := npbindings.AvailableNetworksByID[uint32(c.ChainID)]
	if !ok {
		return "", fmt.Errorf("unknown chain ID %d", c.ChainID)
	}
	return npbindings.GetContractAddress(npbindings.ResultsVerifierGroth16Contract, chainName), nil
}

func (c *Contracts) OrganizationRegistryAddress() (string, error) {
	chainName, ok := npbindings.AvailableNetworksByID[uint32(c.ChainID)]
	if !ok {
		return "", fmt.Errorf("unknown chain ID %d", c.ChainID)
	}
	return npbindings.GetContractAddress(npbindings.OrganizationRegistryContract, chainName), nil
}

// RegisterKnownProcess adds a process ID to the knownProcesses map.
// This is used during initialization to register processes that were
// created before the monitor started, ensuring their events are not filtered out.
func (c *Contracts) RegisterKnownProcess(processID types.ProcessID) {
	c.knownProcessesMutex.Lock()
	defer c.knownProcessesMutex.Unlock()
	c.knownProcesses[processID] = struct{}{}
}

// knownPIDs returns a slice of known process IDs as [types.ProcessIDLen]byte arrays, ready to
// be used as filter topics.
func (c *Contracts) knownPIDs() [][types.ProcessIDLen]byte {
	c.knownProcessesMutex.RLock()
	defer c.knownProcessesMutex.RUnlock()
	pids := make([][types.ProcessIDLen]byte, 0, len(c.knownProcesses))
	for processID := range c.knownProcesses {
		pids = append(pids, processID)
	}
	return pids
}
