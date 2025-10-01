package web3

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const (
	sendMaxAttempts     = 10
	cancelGasLimit      = 21000
	retryBackoff        = 300 * time.Millisecond
	cancelBackoff       = 200 * time.Millisecond
	replacementWaitHint = 400 * time.Millisecond
)

// SendTxWithReplacement builds and sends a transaction with nonce reconciliation
// and replacement (speed-up/cancel) handling.
//   - forBlobs indicates whether the tx being sent uses EIP-4844 (blob) fees.
//   - buildAndSend must construct and SEND a tx using the provided nonce and fees,
//     and return the tx (even if sending errored) along with the error.
func (c *Contracts) SendTxWithReplacement(
	ctx context.Context,
	forBlobs bool,
	buildAndSend func(nonce uint64, fees FeeCaps) (*types.Transaction, error),
) (*common.Hash, error) {
	if c.signer == nil {
		return nil, fmt.Errorf("no signer defined")
	}

	fees, err := c.SuggestInitialFees(ctx, forBlobs)
	if err != nil {
		return nil, fmt.Errorf("initial fees: %w", err)
	}

	attempts := 0
	for attempts < sendMaxAttempts {
		attempts++

		// Always reconcile next expected nonce from provider (pending).
		nextNonce, err := c.nextPendingNonce(ctx)
		if err != nil {
			return nil, fmt.Errorf("pending nonce: %w", err)
		}

		tx, sendErr := buildAndSend(nextNonce, fees)
		if sendErr == nil {
			h := tx.Hash()
			return &h, nil
		}
		// Treat "already known" as success (peer has it; our tx is valid and will be pooled).
		if isAlreadyKnown(sendErr) {
			h := tx.Hash()
			return &h, nil
		}

		// Classify error and react.
		switch {
		case isNonceTooHigh(sendErr):
			// Provider expects lower nonces to be included first; reconcile gaps by canceling them.
			// Re-fetch to get the most up-to-date expected nonce.
			expected, err := c.nextPendingNonce(ctx)
			if err != nil {
				return nil, fmt.Errorf("re-fetch pending nonce: %w", err)
			}
			// If expected advanced beyond what we used, just retry loop (we'll pick up new expected).
			if expected > nextNonce {
				time.Sleep(retryBackoff)
				continue
			}
			// If expected is lower than our attempted nonce, we must fill the gap with cancels.
			for n := expected; n < nextNonce; n++ {
				if err := c.sendCancelTx(ctx, n, fees); err != nil && !isBenignSendErr(err) {
					// If cancel was underpriced, bump and retry once for this nonce.
					if isUnderpriced(err) || isFeeTooLow(err) {
						var bumpErr error
						fees, bumpErr = c.BumpFees(ctx, fees)
						if bumpErr != nil {
							return nil, fmt.Errorf("bump fees for cancel: %w", bumpErr)
						}
						if err2 := c.sendCancelTx(ctx, n, fees); err2 != nil && !isBenignSendErr(err2) {
							return nil, fmt.Errorf("cancel nonce %d failed: %w", n, err2)
						}
					} else {
						return nil, fmt.Errorf("cancel nonce %d failed: %w", n, err)
					}
				}
				time.Sleep(cancelBackoff)
			}
			// After canceling, loop back to re-attempt original send at the new expected nonce.

		case isNonceTooLow(sendErr):
			// Some pending was mined or accepted; just try again (expected nonce will advance).
			time.Sleep(retryBackoff)

		case isUnderpriced(sendErr) || isFeeTooLow(sendErr):
			// Bump fees and retry with same nonce.
			var bumpErr error
			fees, bumpErr = c.BumpFees(ctx, fees)
			if bumpErr != nil {
				return nil, fmt.Errorf("bump fees: %w", bumpErr)
			}
			time.Sleep(replacementWaitHint)

		default:
			return nil, fmt.Errorf("send tx failed: %w", sendErr)
		}
	}

	return nil, fmt.Errorf("exhausted attempts (%d) to send tx with replacement", sendMaxAttempts)
}

func (c *Contracts) nextPendingNonce(ctx context.Context) (uint64, error) {
	return c.cli.PendingNonceAt(ctx, c.AccountAddress())
}

// sendCancelTx sends a 0-value EIP-1559 tx to self with the given nonce and fees,
// replacing any pending tx at that nonce (including blob txs).
func (c *Contracts) sendCancelTx(ctx context.Context, nonce uint64, fees FeeCaps) error {
	to := c.AccountAddress()
	chainID := new(big.Int).SetUint64(c.ChainID)

	inner := &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: fees.TipCap,
		GasFeeCap: fees.FeeCap,
		Gas:       cancelGasLimit,
		To:        &to,
		Value:     big.NewInt(0),
		Data:      nil,
	}
	// Sign with latest signer (London+).
	signed, err := types.SignNewTx((*ecdsa.PrivateKey)(c.signer), types.LatestSignerForChainID(chainID), inner)
	if err != nil {
		return fmt.Errorf("sign cancel tx: %w", err)
	}

	if err := c.cli.SendTransaction(ctx, signed); err != nil {
		return err
	}
	return nil
}

// Error classifiers
func isNonceTooHigh(err error) bool {
	return containsErr(err, "nonce too high")
}

func isNonceTooLow(err error) bool {
	return containsErr(err, "nonce too low")
}

func isUnderpriced(err error) bool {
	return containsErr(err, "replacement transaction underpriced") ||
		containsErr(err, "transaction underpriced") ||
		containsErr(err, "tip too low")
}

func isFeeTooLow(err error) bool {
	return containsErr(err, "fee cap too low") ||
		containsErr(err, "max priority fee per gas higher than max fee per gas") ||
		containsErr(err, "max fee per gas less than block base fee")
}

func isAlreadyKnown(err error) bool {
	return containsErr(err, "already known")
}

func isBenignSendErr(err error) bool {
	return isAlreadyKnown(err) || isNonceTooLow(err)
}

func containsErr(err error, sub string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), strings.ToLower(sub))
}
