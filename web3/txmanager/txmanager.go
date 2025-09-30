package txmanager

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	ethSigner "github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/web3/rpc"
)

const (
	// Default configuration values
	defaultMaxPendingTime     = 5 * time.Minute
	defaultMaxRetries         = 3
	defaultFeeIncreasePercent = 20
	defaultMaxGasPriceGwei    = 300
	defaultMonitorInterval    = 30 * time.Second
)

// Config holds configuration for the transaction manager
type Config struct {
	MaxPendingTime     time.Duration
	MaxRetries         int
	FeeIncreasePercent int
	MaxGasPriceGwei    *big.Int
	MonitorInterval    time.Duration
	ChainID            *big.Int
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
	}
}

// PendingTransaction represents a transaction that has been sent but not yet
// confirmed.
type PendingTransaction struct {
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
	BlobSidecar      *types.BlobTxSidecar // Store sidecar for rebuilding blob txs
}

// TxManager handles nonce management and stuck transaction recovery.
type TxManager struct {
	web3pool *rpc.Web3Pool
	cli      *rpc.Client
	signer   *ethSigner.Signer
	mu       sync.Mutex

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
				if err := tm.handleStuckTransactions(tm.monitorCtx); err != nil {
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

// SendTransactionWithFallback sends a transaction with automatic fallback and recovery mechanisms
func (tm *TxManager) SendTransactionWithFallback(ctx context.Context, txBuilder func(nonce uint64) (*types.Transaction, error)) (*common.Hash, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.nonceInitialized {
		return nil, fmt.Errorf("transaction manager not initialized")
	}

	// Check for stuck transactions before sending new one
	if err := tm.handleStuckTransactions(ctx); err != nil {
		log.Warnw("failed to handle stuck transactions", "error", err)
		// Continue anyway - we'll try to send
	}

	// Use our tracked nonce instead of pending nonce
	nonce := tm.nextNonce

	// Build transaction with our nonce
	tx, err := txBuilder(nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to build transaction: %w", err)
	}

	// Send transaction
	if err := tm.cli.SendTransaction(ctx, tx); err != nil {
		// Check if error is nonce-related
		if strings.Contains(err.Error(), "nonce too high") || strings.Contains(err.Error(), "nonce too low") {
			log.Warnw("nonce mismatch detected, attempting recovery",
				"error", err.Error(),
				"ourNonce", nonce)
			return tm.recoverFromNonceGap(ctx, txBuilder)
		}
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	// Track the transaction
	hash := tx.Hash()
	tm.trackTransaction(tx)
	tm.nextNonce++

	log.Infow("transaction sent",
		"hash", hash.Hex(),
		"nonce", nonce,
		"to", tx.To().Hex())

	return &hash, nil
}

// trackTransaction adds a transaction to the pending list
func (tm *TxManager) trackTransaction(tx *types.Transaction) {
	ptx := &PendingTransaction{
		Hash:      tx.Hash(),
		Nonce:     tx.Nonce(),
		Timestamp: time.Now(),
		To:        *tx.To(),
		Data:      tx.Data(),
		Value:     tx.Value(),
	}

	// Determine transaction type and extract fee information
	switch tx.Type() {
	case types.BlobTxType:
		ptx.IsBlob = true
		// For blob transactions, extract fee cap from transaction
		ptx.OriginalGasPrice = tx.GasFeeCap()
		ptx.OriginalBlobFee = tx.BlobGasFeeCap()
		ptx.BlobHashes = tx.BlobHashes()
		// NOTE: Cannot extract sidecar from transaction after creation due
		// to unexported fields in go-ethereum. Blob sidecars must be stored
		// separately via TrackBlobTransactionWithSidecar() to enable recovery.
		// WARNING: Without the sidecar, stuck blob transactions CANNOT be
		// recovered (they cannot be cancelled like regular txs, only replaced
		// with same blob data).
		log.Debugw("tracking blob transaction (sidecar not stored - recovery not possible)",
			"nonce", tx.Nonce(),
			"blobCount", len(tx.BlobHashes()))
	case types.DynamicFeeTxType:
		ptx.OriginalGasPrice = tx.GasFeeCap()
	case types.LegacyTxType:
		ptx.OriginalGasPrice = tx.GasPrice()
	}

	tm.pendingTxs[tx.Nonce()] = ptx
}

// TrackBlobTransactionWithSidecar tracks a blob transaction with its sidecar
// for potential recovery. This should be called immediately after sending a
// blob transaction if recovery is desired.
func (tm *TxManager) TrackBlobTransactionWithSidecar(tx *types.Transaction, sidecar *types.BlobTxSidecar) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tx.Type() != types.BlobTxType {
		return fmt.Errorf("transaction is not a blob transaction")
	}

	// Check if transaction is already tracked
	ptx, exists := tm.pendingTxs[tx.Nonce()]
	if !exists {
		return fmt.Errorf("transaction not tracked, call SendTransactionWithFallback first")
	}

	// Update with sidecar
	ptx.BlobSidecar = sidecar
	log.Infow("blob transaction sidecar stored for recovery",
		"nonce", tx.Nonce(),
		"hash", tx.Hash().Hex(),
		"blobCount", len(sidecar.Blobs))

	return nil
}

// handleStuckTransactions checks for and handles stuck transactions
func (tm *TxManager) handleStuckTransactions(ctx context.Context) error {
	now := time.Now()

	for nonce, ptx := range tm.pendingTxs {
		// Check if transaction is too old
		if now.Sub(ptx.Timestamp) < tm.config.MaxPendingTime {
			continue
		}

		// Check if transaction was mined
		ethcli, err := tm.cli.EthClient()
		if err != nil {
			log.Warnw("failed to get eth client", "error", err)
			continue
		}
		receipt, err := ethcli.TransactionReceipt(ctx, ptx.Hash)
		if err == nil && receipt != nil {
			// Transaction was mined!
			log.Infow("transaction confirmed",
				"nonce", nonce,
				"hash", ptx.Hash.Hex(),
				"status", receipt.Status)
			delete(tm.pendingTxs, nonce)
			if nonce >= tm.lastConfirmedNonce {
				tm.lastConfirmedNonce = nonce + 1
			}
			continue
		}

		// Transaction is stuck - attempt replacement
		log.Warnw("stuck transaction detected",
			"nonce", nonce,
			"age", now.Sub(ptx.Timestamp),
			"hash", ptx.Hash.Hex(),
			"retries", ptx.RetryCount)

		if err := tm.speedUpTransaction(ctx, ptx); err != nil {
			log.Errorw(err, fmt.Sprintf("failed to speed up transaction for nonce %d", nonce))
		}
	}

	return nil
}

// speedUpTransaction attempts to speed up a stuck transaction by resending
// with higher fees.
func (tm *TxManager) speedUpTransaction(ctx context.Context, ptx *PendingTransaction) error {
	if ptx.RetryCount >= tm.config.MaxRetries {
		log.Warnw("max retries reached, will cancel transaction",
			"nonce", ptx.Nonce)
		return tm.cancelTransaction(ctx, ptx)
	}

	// Calculate new gas price
	increaseFactor := big.NewInt(int64(100 + tm.config.FeeIncreasePercent))
	newGasPrice := new(big.Int).Mul(ptx.OriginalGasPrice, increaseFactor)
	newGasPrice.Div(newGasPrice, big.NewInt(100))

	// Cap at max gas price
	if newGasPrice.Cmp(tm.config.MaxGasPriceGwei) > 0 {
		newGasPrice = new(big.Int).Set(tm.config.MaxGasPriceGwei)
	}

	var newTx *types.Transaction
	var err error

	if ptx.IsBlob {
		// For blob transactions, also increase blob gas fee
		newBlobFee := new(big.Int).Mul(ptx.OriginalBlobFee, increaseFactor)
		newBlobFee.Div(newBlobFee, big.NewInt(100))

		// Check if sidecar is available for rebuilding
		if ptx.BlobSidecar != nil {
			newTx, err = tm.rebuildBlobTransaction(ctx, ptx, newGasPrice, newBlobFee)
			if err != nil {
				log.Errorw(err, "failed to rebuild blob transaction")
				return fmt.Errorf("cannot rebuild blob transaction: %w", err)
			}
			log.Infow("blob transaction rebuilt with higher fees",
				"nonce", ptx.Nonce,
				"newGasPrice", newGasPrice,
				"newBlobFee", newBlobFee)
		} else {
			// CRITICAL: Blob transactions cannot be cancelled. They can only
			// be replaced with another blob transaction using the same blob
			// data. Without the sidecar, we cannot create a replacement, so
			// the transaction is permanently stuck.
			err := fmt.Errorf("blob transaction stuck: nonce=%d hash=%s - sidecar not stored via TrackBlobTransactionWithSidecar",
				ptx.Nonce, ptx.Hash.Hex())
			log.Errorw(err, "blob transaction stuck without recovery option")
			return err
		}
	} else {
		newTx, err = tm.rebuildRegularTransaction(ctx, ptx, newGasPrice)
		if err != nil {
			return fmt.Errorf("failed to rebuild transaction: %w", err)
		}
	}

	// Send replacement
	if err := tm.cli.SendTransaction(ctx, newTx); err != nil {
		return fmt.Errorf("failed to send replacement: %w", err)
	}

	// Update tracking
	ptx.Hash = newTx.Hash()
	ptx.RetryCount++
	ptx.Timestamp = time.Now()
	ptx.OriginalGasPrice = newGasPrice
	if ptx.IsBlob {
		newBlobFee := new(big.Int).Mul(ptx.OriginalBlobFee, increaseFactor)
		newBlobFee.Div(newBlobFee, big.NewInt(100))
		ptx.OriginalBlobFee = newBlobFee
	}

	log.Infow("transaction sped up",
		"nonce", ptx.Nonce,
		"newHash", newTx.Hash().Hex(),
		"newGasPrice", newGasPrice,
		"retry", ptx.RetryCount)

	return nil
}

// rebuildRegularTransaction rebuilds a regular transaction with new gas price.
func (tm *TxManager) rebuildRegularTransaction(
	ctx context.Context,
	ptx *PendingTransaction,
	newGasPrice *big.Int,
) (*types.Transaction, error) {
	// Get gas tip cap
	tipCap, err := tm.cli.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tip cap: %w", err)
	}

	// Create new transaction with same parameters but higher fees
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   tm.config.ChainID,
		Nonce:     ptx.Nonce,
		GasTipCap: tipCap,
		GasFeeCap: newGasPrice,
		Gas:       300000, // Use a reasonable default
		To:        &ptx.To,
		Value:     ptx.Value,
		Data:      ptx.Data,
	})

	// Sign transaction
	signer := types.NewCancunSigner(tm.config.ChainID)
	signed, err := types.SignTx(tx, signer, (*ecdsa.PrivateKey)(tm.signer))
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	return signed, nil
}

// rebuildBlobTransaction rebuilds a blob transaction with higher fees. This
// requires the original sidecar to be stored via TrackBlobTransactionWithSidecar.
func (tm *TxManager) rebuildBlobTransaction(
	ctx context.Context,
	ptx *PendingTransaction,
	newGasPrice *big.Int,
	newBlobFee *big.Int,
) (*types.Transaction, error) {
	if ptx.BlobSidecar == nil {
		return nil, fmt.Errorf("blob sidecar not available for rebuilding")
	}

	// Get gas tip cap
	tipCap, err := tm.cli.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tip cap: %w", err)
	}

	// Estimate gas for the blob transaction
	gasLimit, err := tm.cli.EstimateGas(ctx, ethereum.CallMsg{
		From:          tm.signer.Address(),
		To:            &ptx.To,
		Data:          ptx.Data,
		BlobHashes:    ptx.BlobSidecar.BlobHashes(),
		BlobGasFeeCap: newBlobFee,
	})
	if err != nil {
		// If estimation fails, use a reasonable default
		gasLimit = 300000
		log.Warnw("gas estimation failed for blob tx, using default",
			"error", err,
			"gasLimit", gasLimit)
	}

	// Build the replacement blob transaction with higher fees
	blobTx := &types.BlobTx{
		ChainID:    uint256.NewInt(tm.config.ChainID.Uint64()),
		Nonce:      ptx.Nonce,
		GasTipCap:  uint256.MustFromBig(tipCap),
		GasFeeCap:  uint256.MustFromBig(newGasPrice),
		Gas:        gasLimit,
		To:         ptx.To,
		Value:      uint256.MustFromBig(ptx.Value),
		Data:       ptx.Data,
		BlobFeeCap: uint256.MustFromBig(newBlobFee),
		BlobHashes: ptx.BlobSidecar.BlobHashes(),
		Sidecar:    ptx.BlobSidecar,
	}

	// Create and sign the transaction
	tx := types.NewTx(blobTx)
	signer := types.NewCancunSigner(tm.config.ChainID)
	signed, err := types.SignTx(tx, signer, (*ecdsa.PrivateKey)(tm.signer))
	if err != nil {
		return nil, fmt.Errorf("failed to sign blob transaction: %w", err)
	}

	log.Infow("rebuilt blob transaction with higher fees",
		"nonce", ptx.Nonce,
		"originalBlobFee", ptx.OriginalBlobFee,
		"newBlobFee", newBlobFee,
		"originalGasPrice", ptx.OriginalGasPrice,
		"newGasPrice", newGasPrice)

	return signed, nil
}

// cancelTransaction sends a 0-value transaction to self with higher fees to
// cancel a regular transaction.
// NOTE: This does NOT work for blob transactions! Blob txs can only be
// replaced with another blob tx.
func (tm *TxManager) cancelTransaction(ctx context.Context, ptx *PendingTransaction) error {
	if ptx.IsBlob {
		return fmt.Errorf("cannot cancel blob transaction %s: blob txs can only be replaced with same blob data", ptx.Hash.Hex())
	}
	// Calculate cancellation gas price (2x original)
	gasPrice := new(big.Int).Mul(ptx.OriginalGasPrice, big.NewInt(2))

	// Get tip cap
	tipCap, err := tm.cli.SuggestGasTipCap(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tip cap: %w", err)
	}

	selfAddress := tm.signer.Address()

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   tm.config.ChainID,
		Nonce:     ptx.Nonce,
		GasTipCap: tipCap,
		GasFeeCap: gasPrice,
		Gas:       21000, // standard transfer
		To:        &selfAddress,
		Value:     big.NewInt(0),
		Data:      []byte{},
	})

	signer := types.NewCancunSigner(tm.config.ChainID)
	signed, err := types.SignTx(tx, signer, (*ecdsa.PrivateKey)(tm.signer))
	if err != nil {
		return fmt.Errorf("failed to sign cancel tx: %w", err)
	}

	if err := tm.cli.SendTransaction(ctx, signed); err != nil {
		return fmt.Errorf("failed to send cancel tx: %w", err)
	}

	log.Warnw("transaction cancelled",
		"originalNonce", ptx.Nonce,
		"originalHash", ptx.Hash.Hex(),
		"cancelHash", signed.Hash().Hex())

	ptx.Hash = signed.Hash()
	ptx.Timestamp = time.Now()
	return nil
}

// recoverFromNonceGap attempts to recover from a nonce gap situation
func (tm *TxManager) recoverFromNonceGap(
	ctx context.Context,
	txBuilder func(nonce uint64) (*types.Transaction, error),
) (*common.Hash, error) {
	// Get actual on-chain nonce
	ethcli, err := tm.cli.EthClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get eth client: %w", err)
	}
	onChainNonce, err := ethcli.NonceAt(ctx, tm.signer.Address(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get on-chain nonce: %w", err)
	}

	log.Warnw("nonce gap detected, attempting recovery",
		"ourNextNonce", tm.nextNonce,
		"onChainNonce", onChainNonce,
		"pendingCount", len(tm.pendingTxs))

	// Clear confirmed transactions from pending list
	for nonce := range tm.pendingTxs {
		if nonce < onChainNonce {
			log.Debugw("removing confirmed transaction from pending list", "nonce", nonce)
			delete(tm.pendingTxs, nonce)
		}
	}

	// Find lowest stuck nonce
	lowestStuckNonce := onChainNonce
	for nonce := onChainNonce; nonce < tm.nextNonce; nonce++ {
		if _, exists := tm.pendingTxs[nonce]; exists {
			lowestStuckNonce = nonce
			break
		}
	}

	// If we found a stuck transaction, try to speed it up
	if ptx, exists := tm.pendingTxs[lowestStuckNonce]; exists {
		log.Infow("found stuck transaction, attempting speed up",
			"nonce", lowestStuckNonce,
			"hash", ptx.Hash.Hex())
		if err := tm.speedUpTransaction(ctx, ptx); err != nil {
			log.Errorw(err, "failed to speed up stuck transaction")
		}
		return &ptx.Hash, nil
	}

	// No stuck transaction found, reset our nonce and retry
	log.Warnw("no stuck transaction found, resetting nonce",
		"from", tm.nextNonce,
		"to", onChainNonce)
	tm.nextNonce = onChainNonce
	tm.lastConfirmedNonce = onChainNonce

	// Build and send with corrected nonce
	tx, err := txBuilder(onChainNonce)
	if err != nil {
		return nil, fmt.Errorf("failed to rebuild transaction with corrected nonce: %w", err)
	}

	if err := tm.cli.SendTransaction(ctx, tx); err != nil {
		return nil, fmt.Errorf("failed to send transaction after nonce recovery: %w", err)
	}

	hash := tx.Hash()
	tm.trackTransaction(tx)
	tm.nextNonce++

	log.Infow("transaction sent after nonce recovery",
		"hash", hash.Hex(),
		"nonce", onChainNonce)

	return &hash, nil
}
