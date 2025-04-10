package web3

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strconv"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	bindings "github.com/vocdoni/contracts-z/golang-types/non-proxy"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/signatures/ethereum"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/web3/rpc"
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
	OrganizationRegistry common.Address
	ProcessRegistry      common.Address
	ResultsRegistry      common.Address
}

// Contracts contains the bindings to the deployed contracts.
type Contracts struct {
	ChainID            uint64
	ContractsAddresses *Addresses
	organizations      *bindings.OrganizationRegistry
	processes          *bindings.ProcessRegistry
	web3pool           *rpc.Web3Pool
	cli                *rpc.Client
	signer             *ethereum.Signer

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
func New(web3rpcs []string) (*Contracts, error) {
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
		"lastBlock", lastBlock,
		"numEndpoints", len(web3rpcs),
		"numBlocksToWatch", maxPastBlocksToWatch,
	)

	// calculate the start block to watch
	startBlock := int64(lastBlock) - maxPastBlocksToWatch
	if startBlock < 0 {
		startBlock = 0
	}

	return &Contracts{
		ChainID:                *chainID,
		web3pool:               w3pool,
		cli:                    cli,
		knownProcesses:         make(map[string]struct{}),
		knownOrganizations:     make(map[string]struct{}),
		lastWatchProcessBlock:  uint64(startBlock),
		lastWatchOrgBlock:      uint64(startBlock),
		currentBlock:           lastBlock,
		currentBlockLastUpdate: time.Now(),
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
	organizations, err := bindings.NewOrganizationRegistry(addresses.OrganizationRegistry, c.cli)
	if err != nil {
		return fmt.Errorf("failed to bind organization registry: %w", err)
	}
	process, err := bindings.NewProcessRegistry(addresses.ProcessRegistry, c.cli)
	if err != nil {
		return fmt.Errorf("failed to bind process registry: %w", err)
	}
	c.ContractsAddresses = addresses
	c.processes = process
	c.organizations = organizations
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
	addr, tx, orgBindings, err := bindings.DeployOrganizationRegistry(opts, cli)
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
	c.ContractsAddresses.ProcessRegistry, tx, c.processes, err = bindings.DeployProcessRegistry(opts, cli, strconv.Itoa(int(chainID)), c.ContractsAddresses.OrganizationRegistry)
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
