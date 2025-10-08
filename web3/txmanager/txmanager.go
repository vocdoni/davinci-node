package txmanager

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gtypes "github.com/ethereum/go-ethereum/core/types"
	ethSigner "github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/web3/rpc"
)

const (
	// Default configuration values
	defaultMaxPendingTime     = 5 * time.Minute
	defaultMaxRetries         = 10
	defaultFeeIncreasePercent = 50
	defaultMaxGasPriceGwei    = 300
	defaultMonitorInterval    = 30 * time.Second
	defaultSimpleTxTimeout    = 20 * time.Second
)

// Config holds configuration for the transaction manager
type Config struct {
	MaxPendingTime     time.Duration
	MaxRetries         int
	FeeIncreasePercent int
	MaxGasPriceGwei    *big.Int
	MonitorInterval    time.Duration
	ChainID            *big.Int
	SimpleTxTimeout    time.Duration
}

// DefaultConfig returns a default configuration
func DefaultConfig(chainID uint64) Config {
	return Config{
		MaxPendingTime:     defaultMaxPendingTime,
		MaxRetries:         defaultMaxRetries,
		FeeIncreasePercent: defaultFeeIncreasePercent,
		MaxGasPriceGwei:    new(big.Int).Mul(big.NewInt(defaultMaxGasPriceGwei), big.NewInt(1e9)), // Convert to wei
		MonitorInterval:    defaultMonitorInterval,
		ChainID:            new(big.Int).SetUint64(chainID),
		SimpleTxTimeout:    defaultSimpleTxTimeout,
	}
}

// PendingTransaction represents a transaction that has been sent but not yet
// confirmed.
type PendingTransaction struct {
	ID               []byte
	Hash             common.Hash
	Nonce            uint64
	Timestamp        time.Time
	RetryCount       int
	IsBlob           bool
	OriginalGasPrice *big.Int
	OriginalBlobFee  *big.Int
	To               common.Address
	Data             []byte
	Value            *big.Int
	BlobHashes       []common.Hash
	BlobSidecar      *gtypes.BlobTxSidecar // Store sidecar for rebuilding blob txs
}

// TxManager handles nonce management and stuck transaction recovery.
type TxManager struct {
	web3pool *rpc.Web3Pool
	cli      *rpc.Client
	signer   *ethSigner.Signer
	mu       sync.RWMutex

	// Nonce tracking
	nextNonce          uint64
	lastConfirmedNonce uint64
	nonceInitialized   bool

	// Transaction tracking
	pendingTxs map[uint64]*PendingTransaction

	// Configuration
	config Config

	// Monitoring
	monitorCtx    context.Context
	monitorCancel context.CancelFunc
}

// New creates a new transaction manager and initializes it by fetching the
// current on-chain nonce.
func New(ctx context.Context, web3pool *rpc.Web3Pool, cli *rpc.Client, signer *ethSigner.Signer, config Config) (*TxManager, error) {
	tm := &TxManager{
		web3pool:   web3pool,
		cli:        cli,
		signer:     signer,
		pendingTxs: make(map[uint64]*PendingTransaction),
		config:     config,
	}
	// Get confirmed on-chain nonce (not pending)
	ethcli, err := tm.cli.EthClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get eth client: %w", err)
	}
	nonce, err := ethcli.NonceAt(ctx, tm.signer.Address(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get on-chain nonce: %w", err)
	}
	tm.lastConfirmedNonce = nonce
	tm.nextNonce = nonce
	tm.nonceInitialized = true
	return tm, nil
}

// Start starts the background monitoring of pending transactions.
func (tm *TxManager) Start(ctx context.Context) {
	tm.monitorCtx, tm.monitorCancel = context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(tm.config.MonitorInterval)
		defer ticker.Stop()

		log.Infow("transaction monitor started", "interval", tm.config.MonitorInterval)

		for {
			select {
			case <-tm.monitorCtx.Done():
				log.Infow("transaction monitor stopped")
				return
			case <-ticker.C:
				tm.mu.Lock()
				if err := tm.handleStuckTxs(tm.monitorCtx); err != nil {
					log.Errorw(err, "error handling stuck transactions")
				}
				tm.mu.Unlock()
			}
		}
	}()
}

// Stop stops the background monitoring
func (tm *TxManager) Stop() {
	if tm.monitorCancel != nil {
		tm.monitorCancel()
	}
}

// CheckTxStatusByHash checks the status of a transaction given its ID. Returns
// true if the transaction was successful, false otherwise.
func (tm *TxManager) CheckTxStatusByHash(hash common.Hash) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tm.config.SimpleTxTimeout)
	defer cancel()
	receipt, err := tm.txReceipt(ctx, hash)
	if err != nil {
		return false, fmt.Errorf("failed to get transaction receipt: %w", err)
	}
	return receipt.Status == 1, nil
}

// CheckTxStatusByID checks the status of a transaction given its ID. Returns
// true if the transaction was successful, false otherwise.
func (tm *TxManager) CheckTxStatusByID(id []byte) (bool, error) {
	ptx, exists := tm.PendingTx(id)
	if !exists {
		return false, fmt.Errorf("transaction with id %s not found", fmt.Sprintf("%x", id))
	}
	return tm.CheckTxStatusByHash(ptx.Hash)
}

// PendingTx retrieves a pending transaction by its ID. Returns a copy of the
// transaction (for thread safety) and true if found, or nil and false if not
// found.
func (tm *TxManager) PendingTx(id []byte) (PendingTransaction, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	for _, ptx := range tm.pendingTxs {
		if bytes.Equal(id, ptx.ID) {
			return *ptx, true
		}
	}
	return PendingTransaction{}, false
}

// WaitTxByHash waits for a transaction to be mined identified by its hash. If
// a callback is provided, it runs the wait in the background and calls the
// callback with the result. If no callback is provided, it waits synchronously
// and returns the result.
func (tm *TxManager) WaitTxByHash(hash common.Hash, timeOut time.Duration, cb ...func(error)) error {
	waitFn := func() error {
		timeout := time.After(timeOut)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-timeout:
				return fmt.Errorf("timeout waiting for hash %s", hash.Hex())
			case <-ticker.C:
				// Check if the transaction is mined
				if status, _ := tm.CheckTxStatusByHash(hash); status {
					return nil
				}
			}
		}
	}
	// If callback is provided, run in background with callback
	if len(cb) > 0 {
		go func() {
			err := waitFn()
			cb[0](err)
		}()
		return nil
	}
	// If no callback is provided, run synchronously
	return waitFn()
}

// WaitTxByID waits for a transaction to be mined identified by its ID. If a
// callback is provided, it runs the wait in the background and calls the
// callback with the result. If no callback is provided, it waits synchronously
// and returns the result.
func (tm *TxManager) WaitTxByID(id []byte, timeOut time.Duration, cb ...func(error)) error {
	waitFn := func() error {
		timeout := time.After(timeOut)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-timeout:
				return fmt.Errorf("timeout waiting for id %s", fmt.Sprintf("%x", id))
			case <-ticker.C:
				// Check if the transaction is mined
				if status, _ := tm.CheckTxStatusByID(id); status {
					return nil
				}
			}
		}
	}
	// If callback is provided, run in background with callback
	if len(cb) > 0 {
		go func() {
			err := waitFn()
			cb[0](err)
		}()
		return nil
	}
	// If no callback is provided, run synchronously
	return waitFn()
}

// txReceipt fetches the transaction receipt for a given transaction hash. It
// returns the receipt and any error encountered.
func (tm *TxManager) txReceipt(ctx context.Context, hash common.Hash) (*gtypes.Receipt, error) {
	ethcli, err := tm.cli.EthClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get eth client: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	receipt, err := ethcli.TransactionReceipt(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction receipt: %w", err)
	}
	return receipt, nil
}
