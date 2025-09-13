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

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	npbindings "github.com/vocdoni/davinci-contracts/golang-types"
	vbindings "github.com/vocdoni/davinci-contracts/golang-types/verifiers"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3/rpc"
)

const (
	// web3QueryTimeout is the timeout for web3 queries.
	web3QueryTimeout = 10 * time.Second

	// maxPastBlocksToWatch is the maximum number of past blocks to watch for events.
	maxPastBlocksToWatch = 9990

	// currentBlockIntervalUpdate is the interval to update the current block.
	currentBlockIntervalUpdate = 5 * time.Second
)

// Addresses contains the addresses of the contracts deployed in the network.
type Addresses struct {
	OrganizationRegistry      common.Address
	ProcessRegistry           common.Address
	StateTransitionZKVerifier common.Address
	ResultsZKVerifier         common.Address
}

type ContractABIs struct {
	OrganizationRegistry      *abi.ABI
	ProcessRegistry           *abi.ABI
	StateTransitionZKVerifier *abi.ABI
	ResultsZKVerifier         *abi.ABI
}

// Contracts contains the bindings to the deployed contracts.
type Contracts struct {
	ChainID                  uint64
	ContractsAddresses       *Addresses
	ContractABIs             *ContractABIs
	Web3ConsensusAPIEndpoint string
	organizations            *npbindings.OrganizationRegistry
	processes                *npbindings.ProcessRegistry
	web3pool                 *rpc.Web3Pool
	cli                      *rpc.Client
	signer                   *ethereum.Signer

	currentBlock           uint64
	currentBlockLastUpdate time.Time
	currentBlockMutex      sync.Mutex

	knownProcesses        map[string]struct{}
	lastWatchProcessBlock uint64
	knownOrganizations    map[string]struct{}
	lastWatchOrgBlock     uint64
}

// New creates a new Contracts instance with the given web3 endpoints.
// It initializes the web3 pool and the client, and sets up the known processes
func New(web3rpcs []string, web3cApi string) (*Contracts, error) {
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

	return &Contracts{
		ChainID:                  *chainID,
		web3pool:                 w3pool,
		cli:                      cli,
		Web3ConsensusAPIEndpoint: web3cApi,
		knownProcesses:           make(map[string]struct{}),
		knownOrganizations:       make(map[string]struct{}),
		lastWatchProcessBlock:    uint64(startBlock),
		lastWatchOrgBlock:        uint64(startBlock),
		currentBlock:             lastBlock,
		currentBlockLastUpdate:   time.Now(),
	}, nil
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

// LoadContracts loads the contracts from the given addresses.
func (c *Contracts) LoadContracts(addresses *Addresses) error {
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

	orgRegistryABI, err := abi.JSON(strings.NewReader(npbindings.OrganizationRegistryABI))
	if err != nil {
		return fmt.Errorf("failed to parse organization registry ABI: %w", err)
	}
	processRegistryABI, err := abi.JSON(strings.NewReader(npbindings.ProcessRegistryABI))
	if err != nil {
		return fmt.Errorf("failed to parse process registry ABI: %w", err)
	}
	stVerifierABI, err := abi.JSON(strings.NewReader(vbindings.StateTransitionVerifierGroth16ABI))
	if err != nil {
		return fmt.Errorf("failed to parse zk verifier ABI: %w", err)
	}
	rVerifierABI, err := abi.JSON(strings.NewReader(vbindings.ResultsVerifierGroth16ABI))
	if err != nil {
		return fmt.Errorf("failed to parse zk verifier ABI: %w", err)
	}

	c.ContractABIs = &ContractABIs{
		OrganizationRegistry:      &orgRegistryABI,
		ProcessRegistry:           &processRegistryABI,
		StateTransitionZKVerifier: &stVerifierABI,
		ResultsZKVerifier:         &rVerifierABI,
	}

	return nil
}

// DeployContracts deploys new contracts and returns the bindings.
func DeployContracts(web3rpc, privkey string) (*Contracts, error) {
	w3pool := rpc.NewWeb3Pool()
	chainID, err := w3pool.AddEndpoint(web3rpc)
	if err != nil {
		return nil, fmt.Errorf("failed to add web3 endpoint: %w", err)
	}
	cli, err := w3pool.Client(chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}
	c := &Contracts{
		ChainID:            chainID,
		web3pool:           w3pool,
		cli:                cli,
		knownProcesses:     make(map[string]struct{}),
		knownOrganizations: make(map[string]struct{}),
		ContractsAddresses: &Addresses{},
	}
	if err := c.SetAccountPrivateKey(privkey); err != nil {
		return nil, err
	}

	opts, err := c.authTransactOpts()
	if err != nil {
		return nil, err
	}
	addr, tx, orgBindings, err := npbindings.DeployOrganizationRegistry(opts, cli)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy organization registry: %w", err)
	}
	if err := c.WaitTx(tx.Hash(), web3QueryTimeout); err != nil {
		return nil, err
	}
	c.organizations = orgBindings
	c.ContractsAddresses.OrganizationRegistry = addr
	log.Infow("deployed OrganizationRegistry", "address", addr, "tx", tx.Hash().Hex())

	opts, err = c.authTransactOpts()
	if err != nil {
		return nil, err
	}
	addr, tx, _, err = vbindings.DeployStateTransitionVerifierGroth16(opts, cli)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy state transition zkverifier contract: %w", err)
	}
	if err := c.WaitTx(tx.Hash(), web3QueryTimeout); err != nil {
		return nil, err
	}
	c.ContractsAddresses.StateTransitionZKVerifier = addr
	log.Infow("deployed state transition ZKVerifier contract", "address", addr, "tx", tx.Hash().Hex())

	addr, tx, _, err = vbindings.DeployResultsVerifierGroth16(opts, cli)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy results zkverifier contract: %w", err)
	}
	if err := c.WaitTx(tx.Hash(), web3QueryTimeout); err != nil {
		return nil, err
	}
	c.ContractsAddresses.ResultsZKVerifier = addr
	log.Infow("deployed results ZKVerifier contract", "address", addr, "tx", tx.Hash().Hex())

	opts, err = c.authTransactOpts()
	if err != nil {
		return nil, err
	}
	c.ContractsAddresses.ProcessRegistry, tx, c.processes, err = npbindings.DeployProcessRegistry(
		opts,
		cli,
		uint32(chainID),
		c.ContractsAddresses.StateTransitionZKVerifier,
		c.ContractsAddresses.ResultsZKVerifier,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy process registry: %w", err)
	}
	if err := c.WaitTx(tx.Hash(), web3QueryTimeout); err != nil {
		return nil, err
	}
	log.Infow("deployed ProcessRegistry", "address", c.ContractsAddresses.ProcessRegistry, "tx", tx.Hash().Hex())

	return c, nil
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

// WaitTx waits for a transaction to be mined.
func (c *Contracts) WaitTx(txHash common.Hash, timeOut time.Duration) error {
	for {
		select {
		case <-time.After(timeOut):
			return fmt.Errorf("timeout waiting for tx %s", txHash.Hex())
		default:
			status, _ := c.CheckTxStatus(txHash)
			if status {
				return nil
			}
			time.Sleep(1 * time.Second)
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
	signer, err := ethereum.NewSignerFromHex(hexPrivKey)
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
	auth, err := bind.NewKeyedTransactorWithChainID((*ecdsa.PrivateKey)(c.signer), bChainID)
	if err != nil {
		return nil, fmt.Errorf("failed to create transactor: %w", err)
	}
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

// dev note: this is a temporary method to simulate contract calls
// it works on geth but not expected to work on other clients or external rpc providers
// SimulateContractCall simulates a contract call using the eth_simulateV1 RPC method.
func (c *Contracts) SimulateContractCall(
	ctx context.Context,
	contractAddr common.Address,
	contractABI *abi.ABI,
	method string, args ...any,
) error {
	data, err := contractABI.Pack(method, args...)
	if err != nil {
		return fmt.Errorf("pack %s: %w", method, err)
	}
	auth, err := c.authTransactOpts()
	if err != nil {
		return fmt.Errorf("failed to create transactor: %w", err)
	}

	auth.GasPrice, err = c.cli.SuggestGasPrice(ctx)
	if err != nil {
		return fmt.Errorf("failed to get gas price: %w", err)
	}
	simReq := SimulationRequest{
		BlockStateCalls: []BlockStateCall{
			{
				Calls: []Call{{
					From:     c.signer.Address(),
					To:       contractAddr,
					Data:     data,
					GasPrice: (*hexutil.Big)(big.NewInt(auth.GasPrice.Int64())),
					Nonce:    hexutil.Uint64(auth.Nonce.Uint64()),
				}},
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
	call := simBlocks[0].Calls[0]
	if call.Status == "0x1" {
		// success, nothing to do
		return nil
	}

	reason, uerr := c.decodeRevert(call.Error.Data)
	if uerr != nil {
		return fmt.Errorf("call reverted; failed to unpack reason: %w", uerr)
	}
	return fmt.Errorf("call reverted: %s", reason)
}

// decodeRevert decodes the revert reason from the given data.
func (c *Contracts) decodeRevert(data hexutil.Bytes) (string, error) {
	var errorName string
	err := c.ContractABIs.ForEachABI(func(name string, a *abi.ABI) error {
		for _, e := range a.Errors {
			sig := strings.TrimPrefix(e.String(), "error ")
			hash := crypto.Keccak256([]byte(sig))[:4]
			if bytes.Equal(data, hash) {
				errorName = sig
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if errorName != "" {
		return errorName, nil
	}
	return "", fmt.Errorf("unknown error selector %s", data.String())
}

// ForEachABI calls fn(name, abi) for each non-nil *abi.ABI field.
// Stops and returns an error if fn returns an error.
func (c *ContractABIs) ForEachABI(fn func(fieldName string, a *abi.ABI) error) error {
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
func (c *Contracts) ProcessRegistryABI() (*abi.ABI, error) {
	processRegistryABI, err := abi.JSON(strings.NewReader(npbindings.ProcessRegistryABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse process registry ABI: %w", err)
	}
	return &processRegistryABI, nil
}

// ResultsRegistryABI returns the ABI of the ResultsRegistry contract.
func (c *Contracts) OrganizationRegistryABI() (*abi.ABI, error) {
	organizationRegistryABI, err := abi.JSON(strings.NewReader(npbindings.OrganizationRegistryABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse organization registry ABI: %w", err)
	}
	return &organizationRegistryABI, nil
}

// StateTransitionVerifierABI returns the ABI of the ZKVerifier contract.
func (c *Contracts) StateTransitionVerifierABI() (*abi.ABI, error) {
	stVerifierABI, err := abi.JSON(strings.NewReader(vbindings.StateTransitionVerifierGroth16ABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse state transition zk verifier ABI: %w", err)
	}
	return &stVerifierABI, nil
}

// ResultsVerifierABI returns the ABI of the ResultsVerifier contract.
func (c *Contracts) ResultsVerifierABI() (*abi.ABI, error) {
	resultsVerifierABI, err := abi.JSON(strings.NewReader(vbindings.ResultsVerifierGroth16ABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse results zk verifier ABI: %w", err)
	}
	return &resultsVerifierABI, nil
}
