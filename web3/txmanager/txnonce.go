package txmanager

import (
	"context"
	"fmt"
	"strings"
)

// isNonceError checks if an error is related to nonce issues
func isNonceError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()
	return strings.Contains(errMsg, "nonce too low") ||
		strings.Contains(errMsg, "nonce too high") ||
		strings.Contains(errMsg, "already known")
}

// updateNonceTracking updates internal nonce tracking after a transaction is confirmed
func (tm *TxManager) updateNonceTracking(confirmedNonce uint64) {
	// Remove from pending transactions
	delete(tm.pendingTxs, confirmedNonce)
	// Update last confirmed nonce if this is newer
	if confirmedNonce >= tm.lastConfirmedNonce {
		tm.lastConfirmedNonce = confirmedNonce + 1
	}
	// Ensure nextNonce is at least as high as lastConfirmed
	if tm.nextNonce < tm.lastConfirmedNonce {
		tm.nextNonce = tm.lastConfirmedNonce
	}
}

// lastOnChainNonce retrieves the last confirmed nonce from the blockchain,
// considering both confirmed and pending nonces to ensure accuracy. It returns
// the nonce or an error if the operation fails.
func (tm *TxManager) lastOnChainNonce(ctx context.Context) (uint64, error) {
	ethcli, err := tm.cli.EthClient()
	if err != nil {
		return 0, fmt.Errorf("failed to get eth client: %w", err)
	}
	// Get the last confirmed nonce from the blockchain
	lastNonce, err := ethcli.NonceAt(ctx, tm.signer.Address(), nil)
	if err != nil {
		return 0, fmt.Errorf("failed to get on-chain nonce: %w", err)
	}
	// Also check the pending nonce to avoid "nonce too low" errors
	if pendingNonce, err := tm.cli.PendingNonceAt(ctx, tm.signer.Address()); err == nil && pendingNonce > lastNonce {
		lastNonce = pendingNonce
	}
	return lastNonce, nil
}
