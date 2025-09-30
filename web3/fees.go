package web3

import (
	"context"
	"fmt"
	"math/big"
)

type FeeCaps struct {
	// Execution gas fee caps (EIP-1559)
	TipCap *big.Int // maxPriorityFeePerGas
	FeeCap *big.Int // maxFeePerGas

	// Optional: Blob fee cap (EIP-4844). If nil, treated as non-blob.
	BlobFeeCap *big.Int
}

const (
	minTipBumpGwei    = int64(2) // 2 gwei min absolute bump for tip
	minFeeCapBumpGwei = int64(5) // 5 gwei min absolute bump for fee cap

	// bump factor ~+12.5% (x1.125)
	bumpFactorNum = int64(1125)
	bumpFactorDen = int64(1000)
)

// SuggestInitialFees returns initial FeeCaps built from on-chain conditions.
// If forBlobs is true, it also includes BlobFeeCap (2x blob base fee).
func (c *Contracts) SuggestInitialFees(ctx context.Context, forBlobs bool) (FeeCaps, error) {
	var fees FeeCaps

	tip, err := c.cli.SuggestGasTipCap(ctx)
	if err != nil {
		return fees, fmt.Errorf("suggest tip: %w", err)
	}

	h, err := c.cli.HeaderByNumber(ctx, nil)
	if err != nil {
		return fees, fmt.Errorf("header by number: %w", err)
	}
	if h.BaseFee == nil {
		return fees, fmt.Errorf("no base fee in latest header (pre-london?)")
	}

	feeCap := new(big.Int).Mul(h.BaseFee, big.NewInt(2))
	feeCap.Add(feeCap, tip)

	fees.TipCap = tip
	fees.FeeCap = feeCap

	if forBlobs {
		blobBase, err := c.cli.BlobBaseFee(ctx)
		if err != nil {
			return fees, fmt.Errorf("blob base fee: %w", err)
		}
		fees.BlobFeeCap = new(big.Int).Mul(blobBase, big.NewInt(2))
	}

	return fees, nil
}

// BumpFees bumps the provided FeeCaps using EIP-1559-friendly rules and current base fees.
func (c *Contracts) BumpFees(ctx context.Context, fees FeeCaps) (FeeCaps, error) {
	// Re-suggest tip for sanity, but ensure minimum absolute bump is respected.
	suggestedTip, err := c.cli.SuggestGasTipCap(ctx)
	if err != nil {
		return fees, fmt.Errorf("suggest tip: %w", err)
	}
	// tip' = max(tip * 1.125, tip + 2gwei, suggestedTip)
	tipBumped := maxBig(
		mulFrac(fees.TipCap, bumpFactorNum, bumpFactorDen),
		new(big.Int).Add(fees.TipCap, gwei(minTipBumpGwei)),
		suggestedTip,
	)

	// feeCap' >= base*2 + tip'
	h, err := c.cli.HeaderByNumber(ctx, nil)
	if err != nil {
		return fees, fmt.Errorf("header by number: %w", err)
	}
	baseTarget := new(big.Int).Mul(h.BaseFee, big.NewInt(2))
	baseTarget.Add(baseTarget, tipBumped)

	// feeCap' = max(feeCap * 1.125, feeCap + 5gwei, baseTarget)
	feeCapBumped := maxBig(
		mulFrac(fees.FeeCap, bumpFactorNum, bumpFactorDen),
		new(big.Int).Add(fees.FeeCap, gwei(minFeeCapBumpGwei)),
		baseTarget,
	)

	var blobFeeCapBumped *big.Int
	if fees.BlobFeeCap != nil {
		blobBase, err := c.cli.BlobBaseFee(ctx)
		if err != nil {
			return fees, fmt.Errorf("blob base fee: %w", err)
		}
		blobTarget := new(big.Int).Mul(blobBase, big.NewInt(2))
		blobFeeCapBumped = maxBig(
			mulFrac(fees.BlobFeeCap, bumpFactorNum, bumpFactorDen),
			blobTarget,
		)
	}

	return FeeCaps{
		TipCap:     tipBumped,
		FeeCap:     feeCapBumped,
		BlobFeeCap: blobFeeCapBumped,
	}, nil
}

func mulFrac(x *big.Int, num, den int64) *big.Int {
	if x == nil {
		return nil
	}
	xx := new(big.Int).Set(x)
	xx.Mul(xx, big.NewInt(num))
	xx.Div(xx, big.NewInt(den))
	return xx
}

func maxBig(vals ...*big.Int) *big.Int {
	var best *big.Int
	for _, v := range vals {
		if v == nil {
			continue
		}
		if best == nil || v.Cmp(best) > 0 {
			best = new(big.Int).Set(v)
		}
	}
	if best == nil {
		return big.NewInt(0)
	}
	return best
}

func gwei(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1_000_000_000))
}
