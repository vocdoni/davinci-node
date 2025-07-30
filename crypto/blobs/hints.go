package blobs

import (
	"fmt"
	"math/big"

	"github.com/consensys/gnark/constraint/solver"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/math/emulated"
)

func init() {
	// Register the quotHint, saves 1M constraints.
	solver.RegisterHint(quotHint)
	solver.RegisterHint(splitNativeHint)
	solver.RegisterHint(copyNativeToEmu)
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

	for i := range n {
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

// splitNativeHint is a gnark hint that splits a native BN254 variable
// into an emulated element. It is used to convert a native input
// into an emulated element for further processing.
func splitNativeHint(_ *big.Int, nativeIn, emuOut []*big.Int) error {
	if len(nativeIn) != 1 || len(emuOut) != 1 {
		return fmt.Errorf("splitNativeHint: want 1 in / 1 out")
	}
	emuOut[0].Set(nativeIn[0]) // vBN254 < pBLS so direct copy is safe
	return nil
}

// hintNativeToEmu converts a native BN254 variable to an emulated element
// using the copyNativeToEmu hint.
// It is important to verify that the native input matches the emulated output
// to ensure soundness, in the circuit.
func hintNativeToEmu(_ frontend.API, fr *emulated.Field[FE],
	vNat frontend.Variable,
) (*emulated.Element[FE], error) {
	emu, err := fr.NewHintWithNativeInput(copyNativeToEmu, 1, vNat)
	if err != nil {
		return nil, err
	}
	return emu[0], nil
}

// copyNativeToEmu is a gnark hint that copies a native BN254 variable
// into an emulated element.
func copyNativeToEmu(modNat *big.Int, nativeIn, nativeOut []*big.Int) error {
	// unwrap header, then copy the scalar into the emulated output
	return emulated.UnwrapHintWithNativeInput(nativeIn, nativeOut,
		func(_ *big.Int, nIn, emuOut []*big.Int) error {
			if len(nIn) != 1 || len(emuOut) != 1 {
				return fmt.Errorf("copyNativeToEmu: expect 1/1, got %d/%d",
					len(nIn), len(emuOut))
			}
			emuOut[0].Set(nIn[0]) // v < pBN254 < pBLS12‑381
			return nil
		})
}
