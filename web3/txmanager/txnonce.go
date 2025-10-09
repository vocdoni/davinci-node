package txmanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/vocdoni/davinci-node/log"
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

	lastNonce, err := ethcli.NonceAt(ctx, tm.signer.Address(), nil)
	if err != nil {
		return 0, fmt.Errorf("failed to get on-chain nonce: %w", err)
	}

	pendingNonce, err := tm.cli.PendingNonceAt(ctx, tm.signer.Address())
	if err != nil {
		log.Warnw("failed to get pending nonce, using confirmed nonce", "error", err)
	} else if pendingNonce > lastNonce {
		// Use the higher nonce to prevent "nonce too low" errors
		lastNonce = pendingNonce
		log.Infow("using pending nonce for recovery",
			"pendingNonce", pendingNonce,
			"confirmedNonce", lastNonce)
	}
	return lastNonce, nil
}
