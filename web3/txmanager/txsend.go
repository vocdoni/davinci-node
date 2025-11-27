package txmanager

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

// permanentErrorPatterns defines error patterns that indicate permanent
// failures that should not be retried. These are typically contract-level
// rejections that will never succeed regardless of gas price or retries.
// Add new patterns here as they are discovered and confirmed.
var permanentErrorPatterns = []string{
	"execution reverted", // Contract rejected the transaction
}

// isPermanentError checks if an error represents a permanent failure that
// should not be retried.
func isPermanentError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	for _, pattern := range permanentErrorPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}

// SendTx sends a transaction with automatic fallback and recovery mechanisms.
// It accepts a transaction builder function that takes a nonce and returns a
// signed transaction. If a nonce mismatch is detected, it attempts to recover
// by querying the actual on-chain nonce and resending the transaction. It
// returns the transaction ID or an error.
func (tm *TxManager) SendTx(
	ctx context.Context,
	txBuilder func(nonce uint64) (*gethtypes.Transaction, error),
) (types.HexBytes, *common.Hash, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if !tm.nonceInitialized {
		return nil, nil, fmt.Errorf("transaction manager not initialized")
	}
	// Generate a unique ID for this transaction
	id := util.RandomBytes(32)
	// Check for stuck transactions before sending new one, continue anyway
	// we'll try to send
	if err := tm.handleStuckTxs(ctx); err != nil {
		log.Warnw("failed to handle stuck transactions",
			"error", err)
	}
	// Use our tracked nonce instead of pending nonce
	nonce, err := tm.cli.PendingNonceAt(ctx, tm.signer.Address())
	if err != nil {
		log.Warnw("failed to get pending nonce, using the our last next nonce",
			"error", err,
			"ourNextNonce", tm.nextNonce)
		nonce = tm.nextNonce
	}
	// Build transaction with our nonce
	tx, err := txBuilder(nonce)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build transaction: %w", err)
	}
	// Send transaction
	if err := tm.cli.SendTransaction(ctx, tx); err != nil {
		// Check if error is nonce-related
		if isNonceError(err) {
			log.Warnw("nonce mismatch detected, attempting recovery",
				"error", err.Error(),
				"ourNonce", nonce)
			// Attempt recovery
			hash, err := tm.recoverTxFromNonceGap(ctx, id, txBuilder)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to recover from nonce gap: %w", err)
			}
			// Recovery succeeded, return the id of the transaction and the
			// hash of the sent transaction
			return id, hash, nil
		}
		return nil, nil, fmt.Errorf("failed to send transaction: %w", err)
	}
	// Track the transaction
	hash := tx.Hash()
	tm.trackTx(id, tx)
	tm.nextNonce++
	log.Infow("transaction sent",
		"nonce", nonce,
		"hash", hash.Hex(),
		"id", fmt.Sprintf("%x", id),
		"to", tx.To().Hex())
	return id, &hash, nil
}

// TrackBlobTxWithSidecar tracks a blob transaction with its sidecar for
// potential recovery. This should be called immediately after sending a blob
// transaction if recovery is desired.
func (tm *TxManager) TrackBlobTxWithSidecar(tx *gethtypes.Transaction, sidecar *gethtypes.BlobTxSidecar) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tx.Type() != gethtypes.BlobTxType {
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

// trackTx adds a transaction to the pending list for tracking and potential
// recovery. It extracts necessary information from the transaction and stores
// it in the pending transactions map.
func (tm *TxManager) trackTx(id []byte, tx *gethtypes.Transaction) {
	ptx := &PendingTransaction{
		ID:               id,
		Hash:             tx.Hash(),
		Nonce:            tx.Nonce(),
		Timestamp:        time.Now(),
		OriginalGasLimit: tx.Gas(),
		To:               *tx.To(),
		Data:             tx.Data(),
		Value:            tx.Value(),
	}
	// Determine transaction type and extract fee information
	switch tx.Type() {
	case gethtypes.BlobTxType:
		ptx.IsBlob = true
		// For blob transactions, extract fee cap from transaction
		ptx.OriginalGasPrice = tx.GasFeeCap()
		ptx.OriginalBlobFee = tx.BlobGasFeeCap()
		ptx.BlobHashes = tx.BlobHashes()
		// NOTE: Cannot extract sidecar from transaction after creation due
		// to unexported fields in go-ethereum. Blob sidecars must be stored
		// separately via TrackBlobTxWithSidecar() to enable recovery.
		// WARNING: Without the sidecar, stuck blob transactions CANNOT be
		// recovered (they cannot be cancelled like regular txs, only replaced
		// with same blob data).
		log.Debugw("tracking blob transaction (sidecar not stored - recovery not possible)",
			"nonce", tx.Nonce(),
			"blobCount", len(tx.BlobHashes()))
	case gethtypes.DynamicFeeTxType:
		ptx.OriginalGasPrice = tx.GasFeeCap()
	case gethtypes.LegacyTxType:
		ptx.OriginalGasPrice = tx.GasPrice()
	}
	tm.pendingTxs[tx.Nonce()] = ptx
}

// handleStuckTxs checks for and handles stuck transactions. If a transaction
// has been pending longer than MaxPendingTime, it attempts to speed it up or
// cancel it if max retries are reached. It returns an error if the operation
// fails.
func (tm *TxManager) handleStuckTxs(ctx context.Context) error {
	for nonce, ptx := range tm.pendingTxs {
		// Check if transaction is too old
		txAge := time.Since(ptx.Timestamp)
		if txAge < tm.config.MaxPendingTime {
			continue
		}
		// Check if transaction was mined
		mined, err := tm.CheckTxStatusByHash(ptx.Hash)
		if err == nil && mined {
			log.Infow("transaction confirmed",
				"nonce", nonce,
				"hash", ptx.Hash.Hex())
			tm.updateNonceTracking(nonce)
			continue
		}
		// Store the error for categorization
		if err != nil {
			ptx.LastError = err
		}
		// Transaction is stuck - attempt replacement
		log.Warnw("stuck transaction detected",
			"id", fmt.Sprintf("%x", ptx.ID),
			"nonce", nonce,
			"age", txAge,
			"hash", ptx.Hash.Hex(),
			"retries", ptx.RetryCount)
		if err := tm.speedUpTx(ctx, ptx); err != nil {
			log.Errorw(err, fmt.Sprintf("failed to speed up transaction for nonce %d", nonce))
		}
	}

	return nil
}

// speedUpTx attempts to speed up a stuck transaction by resending with higher
// fees. If max retries are reached, it cancels the transaction if possible.
// It returns an error if the operation fails.
func (tm *TxManager) speedUpTx(ctx context.Context, ptx *PendingTransaction) error {
	// Check if last error was permanent - no point in retrying
	if isPermanentError(ptx.LastError) {
		log.Warnw("transaction failed with permanent error, not retrying",
			"id", fmt.Sprintf("%x", ptx.ID),
			"nonce", ptx.Nonce,
			"error", ptx.LastError.Error())
		// For non-blob txs, cancel them to free the nonce
		if !ptx.IsBlob {
			if err := tm.cancelTx(ctx, ptx); err != nil {
				log.Errorw(err, "failed to cancel transaction with permanent error")
			}
		} else {
			// For blob txs, we can't cancel - just remove from tracking
			log.Warnw("blob transaction permanently failed, removing from tracking",
				"id", fmt.Sprintf("%x", ptx.ID),
				"nonce", ptx.Nonce)
		}
		delete(tm.pendingTxs, ptx.Nonce)
		return fmt.Errorf("transaction permanently failed: %w", ptx.LastError)
	}

	if ptx.RetryCount >= tm.config.MaxRetries {
		// Max retries reached, cancel transaction
		return tm.cancelTx(ctx, ptx)
	}
	// Check if transaction was already mined
	onChainNonce, err := tm.lastOnChainNonce(ctx)
	if err != nil {
		return fmt.Errorf("failed to get last on-chain nonce: %w", err)
	}
	if onChainNonce > ptx.Nonce {
		// pending nonce already advanced on-chain, skipping speed-up
		tm.updateNonceTracking(ptx.Nonce)
		return nil
	}
	// Calculate new gas price
	increaseFactor := big.NewInt(int64(100 + tm.config.FeeIncreasePercent))
	newGasPrice := new(big.Int).Mul(ptx.OriginalGasPrice, increaseFactor)
	newGasPrice.Div(newGasPrice, big.NewInt(100))
	// Cap at max gas price
	if newGasPrice.Cmp(tm.config.MaxGasPriceGwei) > 0 {
		newGasPrice = new(big.Int).Set(tm.config.MaxGasPriceGwei)
	}
	var newTx *gethtypes.Transaction
	switch ptx.IsBlob {
	case true:
		// For blob transactions, also increase blob gas fee
		newBlobFee := new(big.Int).Mul(ptx.OriginalBlobFee, increaseFactor)
		newBlobFee.Div(newBlobFee, big.NewInt(100))
		// Check if sidecar is available for rebuilding
		if ptx.BlobSidecar == nil {
			// CRITICAL: Blob transactions cannot be cancelled. They can only
			// be replaced with another blob transaction using the same blob
			// data. Without the sidecar, we cannot create a replacement, so
			// the transaction is permanently stuck.
			return fmt.Errorf("blob transaction stuck without recovery option")
		}
		// Rebuild blob transaction with higher fees
		if newTx, err = tm.buildTx(ctx, ptx, newGasPrice, newBlobFee); err != nil {
			return fmt.Errorf("cannot rebuild blob transaction: %w", err)
		}
	case false:
		if newTx, err = tm.buildTx(ctx, ptx, newGasPrice, nil); err != nil {
			return fmt.Errorf("failed to rebuild transaction: %w", err)
		}
	}
	// Send replacement
	if err := tm.cli.SendTransaction(ctx, newTx); err != nil {
		if isNonceError(err) {
			// replacement unnecessary, nonce already advanced
			tm.updateNonceTracking(ptx.Nonce)
			return nil
		}
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
		"id", fmt.Sprintf("%x", ptx.ID),
		"nonce", ptx.Nonce,
		"newHash", newTx.Hash().Hex(),
		"newGasPrice", newGasPrice,
		"retry", ptx.RetryCount)
	return nil
}

// cancelTx sends a 0-value transaction to self with higher fees to cancel a
// regular transaction.
// NOTE: This does NOT work for blob transactions. Blob txs can only be
// replaced with another blob tx.
func (tm *TxManager) cancelTx(ctx context.Context, ptx *PendingTransaction) error {
	if ptx.IsBlob {
		return fmt.Errorf("cannot cancel blob transaction %s: blob txs can only be replaced with same blob data", ptx.Hash.Hex())
	}
	// Calculate cancellation gas price (2x original)
	gasPrice := new(big.Int).Mul(ptx.OriginalGasPrice, big.NewInt(2))
	// Build and sign cancellation transaction
	signed, err := tm.buildTx(ctx, &PendingTransaction{
		Nonce: ptx.Nonce,
		To:    tm.signer.Address(),
		Value: big.NewInt(0),
		Data:  []byte{},
	}, gasPrice, nil)
	if err != nil {
		return fmt.Errorf("failed to sign cancel tx: %w", err)
	}
	if err := tm.cli.SendTransaction(ctx, signed); err != nil {
		if isNonceError(err) {
			tm.updateNonceTracking(ptx.Nonce)
			return nil
		}
		return fmt.Errorf("failed to send cancel tx: %w", err)
	}
	log.Warnw("transaction cancelled",
		"id", fmt.Sprintf("%x", ptx.ID),
		"originalNonce", ptx.Nonce,
		"originalHash", ptx.Hash.Hex(),
		"cancelHash", signed.Hash().Hex())
	ptx.Hash = signed.Hash()
	ptx.Timestamp = time.Now()
	return nil
}

// recoverTxFromNonceGap attempts to recover from a nonce gap situation by
// querying the actual on-chain nonce and resending the transaction with the
// correct nonce. It returns the hash of the sent transaction or an error.
func (tm *TxManager) recoverTxFromNonceGap(
	ctx context.Context,
	id []byte,
	txBuilder func(nonce uint64) (*gethtypes.Transaction, error),
) (*common.Hash, error) {
	// Get actual on-chain nonce
	onChainNonce, err := tm.lastOnChainNonce(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get on-chain nonce: %w", err)
	}
	// Clear confirmed transactions from pending list
	for nonce := range tm.pendingTxs {
		if nonce < onChainNonce {
			// removing confirmed transaction from pending list
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
			"id", fmt.Sprintf("%x", ptx.ID),
			"nonce", lowestStuckNonce,
			"hash", ptx.Hash.Hex())
		if err := tm.speedUpTx(ctx, ptx); err != nil {
			log.Errorw(err, "failed to speed up stuck transaction")
		}
		return &ptx.Hash, nil
	}
	// No stuck transaction found, reset our nonce and retry
	tm.nextNonce = onChainNonce
	tm.lastConfirmedNonce = onChainNonce
	// Add a small delay to ensure node nonce caches are updated
	time.Sleep(sleepForNonceCache)
	// Build and send with corrected nonce
	tx, err := txBuilder(onChainNonce)
	if err != nil {
		return nil, fmt.Errorf("failed to rebuild transaction with corrected nonce: %w", err)
	}
	// Double-check the nonce in the built transaction
	if tx.Nonce() != onChainNonce {
		return nil, fmt.Errorf("built transaction has incorrect nonce: %d != %d", tx.Nonce(), onChainNonce)
	}
	// Try multiple times if needed
	var sendErr error
	for attempt := range defaultMaxRetries {
		if attempt > 0 {
			log.Infow("retrying send after nonce recovery", "attempt", attempt+1)
			time.Sleep(time.Second)
		}
		if sendErr = tm.cli.SendTransaction(ctx, tx); sendErr != nil {
			log.Warnw("failed to send transaction after nonce recovery",
				"error", sendErr,
				"attempt", attempt+1)
			if isPermanentError(err) {
				return nil, fmt.Errorf("permanent error sending transaction after nonce recovery: %w", sendErr)
			}
			continue
		}
		// Success
		hash := tx.Hash()
		tm.trackTx(id, tx)
		tm.nextNonce++
		log.Infow("transaction sent after nonce recovery",
			"hash", hash.Hex(),
			"nonce", onChainNonce,
			"nextNonce", tm.nextNonce,
			"attempt", attempt+1)
		return &hash, nil
	}
	return nil, fmt.Errorf("failed to send transaction after nonce recovery after 3 attempts: %w", sendErr)
}
