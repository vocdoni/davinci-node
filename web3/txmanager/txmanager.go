package txmanager

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	gtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
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

	// Small delay after nonce reset to allow node caches to update, only for
	// internal use.
	sleepForNonceCache = 500 * time.Millisecond
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
	GasEstimateOpts    *GasEstimateOpts
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
		GasEstimateOpts:    DefaultGasEstimateOpts,
	}
}

// PendingTransaction represents a transaction that has been sent but not yet
// confirmed.
type PendingTransaction struct {
	// ID is a unique identifier for the transaction. It does not change even
	// if the transaction is retried with a different gas price or nonce.
	ID []byte
	// Hash is the current transaction hash. It may change if the transaction
	// is retried or cancelled.
	Hash common.Hash
	// Nonce is the transaction nonce. It may change if some nonce issue arises.
	Nonce uint64
	// Time the transaction was first sent.
	Timestamp time.Time
	// RetryCount is the number of times the transaction has been retried.
	RetryCount int
	// IsBlob indicates if the transaction is a blob transaction.
	IsBlob bool
	// OriginalGasPrice, OriginalBlobFee and OriginalGasLimit store the
	// original parameters of the transaction for reference in case of retries.
	// They do not change.
	OriginalGasPrice *big.Int
	OriginalBlobFee  *big.Int
	OriginalGasLimit uint64
	// To, Data and Value store the basic parameters of the transaction.
	To    common.Address
	Data  []byte
	Value *big.Int
	// BlobHashes store the hashes of the blobs associated with the transaction.
	BlobHashes []common.Hash
	// BlobSidecar stores the sidecar for rebuilding blob transactions.
	BlobSidecar *gtypes.BlobTxSidecar
	// LastError stores the last error encountered for this transaction.
	// Used to categorize failures as permanent or temporary.
	LastError error
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

	// Gas estimation cache
	gasCache   map[string]uint64
	gasCacheMu sync.RWMutex

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
		gasCache:   make(map[string]uint64),
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

// BuildDynamicFeeTx builds a standard EIP-1559 transaction with the given
// nonce and parameters. It fetches the current gas price and tip cap,
// estimates gas, and signs the transaction. It returns the signed transaction
// or an error if any step fails.
func (tm *TxManager) BuildDynamicFeeTx(
	ctx context.Context,
	to common.Address,
	data []byte,
	nonce uint64,
) (*gtypes.Transaction, error) {
	return tm.buildTx(ctx, &PendingTransaction{
		To:    to,
		Data:  data,
		Nonce: nonce,
	}, nil, nil)
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

// buildTx builds and signs a transaction based on the provided pending
// transaction details, gas fee cap, and blob fee. It estimates gas,
// constructs the appropriate transaction type (standard or blob), and
// signs it. If no gas fee cap is provided, it calculates one based on
// current network conditions.
func (tm *TxManager) buildTx(
	ctx context.Context,
	ptx *PendingTransaction,
	gasFeeCap *big.Int,
	blobFee *big.Int,
) (*gtypes.Transaction, error) {
	// Get gas price and tip
	tipCap, err := tm.cli.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tip cap: %w", err)
	}
	// If no gas fee cap provided, calculate it
	if gasFeeCap == nil {
		baseFee, err := tm.cli.SuggestGasPrice(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get base fee: %w", err)
		}
		// Cap gas fee (baseFee * 2 + tipCap)
		gasFeeCap = new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(2)), tipCap)
	}
	// Initialize call message for gas estimation
	estimateMsg := ethereum.CallMsg{
		From:      tm.signer.Address(),
		To:        &ptx.To,
		GasTipCap: tipCap,
		GasFeeCap: gasFeeCap,
		Data:      ptx.Data,
	}
	// Include blob parameters if applicable
	if blobFee != nil {
		estimateMsg.BlobHashes = ptx.BlobSidecar.BlobHashes()
		estimateMsg.BlobGasFeeCap = blobFee
	}
	// Estimate gas for the transaction
	gasLimit, err := tm.EstimateGas(ctx, estimateMsg, tm.config.GasEstimateOpts, ptx.OriginalGasLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to estimate gas: %w", err)
	}

	var tx *gtypes.Transaction
	if blobFee != nil {
		// Build blob transaction
		tx = gtypes.NewTx(&gtypes.BlobTx{
			ChainID:    uint256.NewInt(tm.config.ChainID.Uint64()),
			Nonce:      ptx.Nonce,
			GasTipCap:  uint256.MustFromBig(tipCap),
			GasFeeCap:  uint256.MustFromBig(gasFeeCap),
			Gas:        gasLimit,
			To:         ptx.To,
			Value:      uint256.MustFromBig(ptx.Value),
			Data:       ptx.Data,
			BlobFeeCap: uint256.MustFromBig(blobFee),
			BlobHashes: ptx.BlobSidecar.BlobHashes(),
			Sidecar:    ptx.BlobSidecar,
		})
	} else {
		// Build standard dynamic fee transaction
		tx = gtypes.NewTx(&gtypes.DynamicFeeTx{
			ChainID:   tm.config.ChainID,
			Nonce:     ptx.Nonce,
			GasTipCap: tipCap,
			GasFeeCap: gasFeeCap,
			Gas:       gasLimit,
			To:        &ptx.To,
			Value:     ptx.Value,
			Data:      ptx.Data,
		})
	}
	// Sign transaction
	signed, err := tm.signTx(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}
	return signed, nil
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

// signTx signs a transaction with the configured signer
func (tm *TxManager) signTx(tx *gtypes.Transaction) (*gtypes.Transaction, error) {
	signer := gtypes.NewCancunSigner(tm.config.ChainID)
	signed, err := gtypes.SignTx(tx, signer, (*ecdsa.PrivateKey)(tm.signer))
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}
	return signed, nil
}
