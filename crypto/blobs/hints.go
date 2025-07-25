package blobs

import (
	"math/big"

	"github.com/consensys/gnark/constraint/solver"
	"github.com/consensys/gnark/std/math/emulated"
)

func init() {
	// Register the quotHint, saves 1M constraints.
	solver.RegisterHint(quotHint)
}

// quotHint is the gnark hint wrapper.  It unwraps the emulated limbs,
// runs the computeQuot function, and re‑wraps the result.
func quotHint(modNative *big.Int, inNative, outNative []*big.Int) error {
	return emulated.UnwrapHint(inNative, outNative, computeQuot)
}

// computeQuot runs over canonical big.Int values (all already < r).
//
//	Inputs  : [ Y | Z | d₀,…,d_{N−1} | ω₀,…,ω_{N−1} ]
//	Outputs : [ q₀,…,q_{N−1},  Σ qᵢ·ωᵢ ]
//
// It implements:
//
//	      dᵢ−Y
//	qᵢ = ───────┬─────────────────────────────
//	     ωᵢ−Z   │ if  ωᵢ ≠ Z
//	     0      │ otherwise
func computeQuot(mod *big.Int, in, out []*big.Int) error {
	Y := new(big.Int).Set(in[0])
	Z := new(big.Int).Set(in[1])

	n := (len(in) - 2) / 2
	dVals := in[2 : 2+n]
	wVals := in[2+n : 2+2*n]

	sum := new(big.Int) // Σ qᵢ·ωᵢ (mod r)

	// Work variables.
	num := new(big.Int)
	den := new(big.Int)
	qi := new(big.Int)
	tmp := new(big.Int)

	for i := 0; i < n; i++ {
		// num =  dᵢ − Y  (mod r)
		num.Sub(dVals[i], Y).Mod(num, mod)

		// den =  ωᵢ − Z  (mod r)
		den.Sub(wVals[i], Z).Mod(den, mod)

		if den.Sign() != 0 {
			qi.ModInverse(den, mod) // qi = 1/(ωᵢ−Z)
			qi.Mul(qi, num).Mod(qi, mod)
		} else {
			qi.SetUint64(0)
		}
		out[i] = new(big.Int).Set(qi) // keep ownership

		// running sum Σ qᵢ·ωᵢ
		tmp.Mul(qi, wVals[i]).Mod(tmp, mod)
		sum.Add(sum, tmp).Mod(sum, mod)
	}

	out[n] = sum
	return nil
}
