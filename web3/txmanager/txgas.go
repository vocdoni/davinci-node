package txmanager

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/web3/rpc"
)

const (
	// DefaultGasFallback is the final fallback gas limit if all estimation
	// methods fail.
	DefaultGasFallback = 300_000
	// DefaultCancelGasFallback is the gas limit used for cancel transactions
	// if estimation fails.
	DefaultCancelGasFallback = 21_000
	// DefaultEstimateGasTimeout is the timeout for gas estimation calls.
	DefaultEstimateGasTimeout = 20 * time.Second
)

// GasEstimateOpts allows tuning of estimator behavior
type GasEstimateOpts struct {
	MinGas    uint64        // minimum possible gas limit (default 21,000)
	MaxGas    uint64        // maximum possible gas limit (default 5,000,000)
	SafetyBps int           // safety margin in basis points (default +10%)
	Timeout   time.Duration // timeout for each estimation call (default 20s)
	Fallback  uint64        // final fallback gas (default 300,000)
}

// DefaultGasEstimateOpts provides a reasonable default configuration for
// gas estimation. It includes a safety margin (10%) and retries (5), and
// sets sensible min (21,000)/max (5,000,000) limits. It also defines a
// fallback gas limit (300,000) if all estimation methods fail.
var DefaultGasEstimateOpts = &GasEstimateOpts{
	MinGas:    21_000,
	MaxGas:    5_000_000,
	SafetyBps: 1000,
	Timeout:   DefaultEstimateGasTimeout,
	Fallback:  DefaultGasFallback,
}

// validate method ensures the options are valid, setting defaults where
// needed, even if the receiver is nil.
func (o *GasEstimateOpts) validate() {
	if o.MinGas == 0 {
		o.MinGas = DefaultGasEstimateOpts.MinGas
	}
	if o.MaxGas == 0 {
		o.MaxGas = DefaultGasEstimateOpts.MaxGas
	}
	if o.SafetyBps == 0 {
		o.SafetyBps = DefaultGasEstimateOpts.SafetyBps
	}
	if o.Timeout == 0 {
		o.Timeout = DefaultGasEstimateOpts.Timeout
	}
	if o.Fallback == 0 {
		o.Fallback = DefaultGasEstimateOpts.Fallback
	}
}

// EstimateGas attempts to estimate the gas limit for a transaction using
// multiple strategies to improve reliability. It first tries the standard
// EstimateGas method, retrying on failure. If that fails, it falls back to a
// binary search using eth_call to find the minimum gas limit that does not
// revert. It applies a safety margin and clamps the result within configured
// limits. If a non-nil TxManager is passed, it also caches successful estimates
// based on the call message to optimize future calls.
func EstimateGas(
	ctx context.Context,
	cli *rpc.Client,
	tm *TxManager,
	msg ethereum.CallMsg,
	opts *GasEstimateOpts,
	floorGasLimit uint64,
) (uint64, error) {
	// Validate configuration
	if opts == nil {
		opts = DefaultGasEstimateOpts
	}
	opts.validate()
	// Create a context with timeout for estimation calls
	internalCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	// Ensure fee caps exist for dynamic fee calls
	if msg.GasFeeCap == nil || msg.GasTipCap == nil {
		// Get tip cap
		tipCap, err := cli.SuggestGasTipCap(internalCtx)
		if err != nil {
			log.Warnw("failed to get tip cap", "error", err)
		}
		// Get base fee
		baseFee, err := cli.SuggestGasPrice(internalCtx)
		if err != nil {
			log.Warnw("failed to get base fee", "error", err)
		}
		// Set fee caps if we obtained them
		if tipCap != nil && baseFee != nil {
			msg.GasTipCap = tipCap
			msg.GasFeeCap = new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(2)), tipCap)
		}
	}

	if gas, err := cli.EstimateGas(internalCtx, msg); err == nil {
		return applySafetyMargin(gas, floorGasLimit, opts), nil
	} else {
		log.Warnw("estimateGas failed, falling back to binary search", "error", err)
	}

	// Try a lightweight binary search with eth_call
	if ethcli, err := cli.EthClient(); err == nil {
		low, high := opts.MinGas, opts.MaxGas
		if tm != nil {
			if cached := tm.cachedGasHint(msg); cached > 0 {
				if cached/2 > low {
					low = cached / 2
				}
				if cached*2 < high {
					high = cached * 2
				}
			}
		}
		// Function to test if a given gas limit works with eth_call
		succeeds := func(limit uint64) bool {
			msg.Gas = limit
			_, callErr := ethcli.CallContract(internalCtx, msg, nil)
			return callErr == nil
		}
		// Check boundaries first (low and high)
		if succeeds(low) {
			return applySafetyMargin(low, floorGasLimit, opts), nil
		}
		if !succeeds(high) {
			log.Warnw("gas estimation binary search failed (revert or logic error)",
				"fallback", opts.Fallback)
			return opts.Fallback, nil
		}
		// Binary search between low and high
		for low+1000 < high {
			mid := (low + high) / 2
			if succeeds(mid) {
				high = mid
			} else {
				low = mid + 1
			}
		}
		// Store result in cache
		if tm != nil {
			tm.storeGasHint(msg, high)
		}
		// Return result with safety margin
		return applySafetyMargin(high, floorGasLimit, opts), nil
	}

	// Absolute fallback
	log.Warnw("all gas estimation methods failed, using fallback",
		"fallback", opts.Fallback)
	return opts.Fallback, nil
}

// applySafetyMargin adds a safety buffer and clamps to limits
func applySafetyMargin(gas, floor uint64, o *GasEstimateOpts) uint64 {
	gas += (gas * uint64(o.SafetyBps)) / 10_000
	if gas < o.MinGas {
		gas = o.MinGas
	}
	if gas < floor {
		gas = floor
	}
	if gas > o.MaxGas {
		gas = o.MaxGas
	}
	return gas
}

// cachedGasHint retrieves a cached gas hint for the given call message,
// if any, returning 0 if none exists.
func (tm *TxManager) cachedGasHint(msg ethereum.CallMsg) uint64 {
	tm.gasCacheMu.RLock()
	defer tm.gasCacheMu.RUnlock()
	if tm.gasCache == nil {
		return 0
	}
	if v, ok := tm.gasCache[gasKey(msg)]; ok {
		return v
	}
	return 0
}

// storeGasHint stores a gas hint for the given call message in the cache to
// optimize future estimations.
func (tm *TxManager) storeGasHint(msg ethereum.CallMsg, gas uint64) {
	tm.gasCacheMu.Lock()
	defer tm.gasCacheMu.Unlock()
	if tm.gasCache == nil {
		return
	}
	tm.gasCache[gasKey(msg)] = gas
}

// gasKey generates a unique key for a CallMsg based on its To address and
// the first 4 bytes of its data (function selector). If the data is less
// than 4 bytes, it hashes the entire data along with the To address to
// create a unique key.
func gasKey(msg ethereum.CallMsg) string {
	if msg.To != nil && len(msg.Data) >= 4 {
		return msg.To.Hex() + "|" + common.Bytes2Hex(msg.Data[:4])
	}
	h := sha256.New()
	if msg.To != nil {
		h.Write(msg.To.Bytes())
	}
	h.Write(msg.Data)
	return fmt.Sprintf("%x", h.Sum(nil))
}
