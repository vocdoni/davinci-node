package txmanager

import "math/big"

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
