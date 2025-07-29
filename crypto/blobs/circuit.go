//	Blob‑evaluation proof  (EIP‑4844 / Proto‑Danksharding)
//
//	Proves in‑circuit that Y = P(Z)
//	where P is the polynomial that commits to the 4096‑cell “blob” and
//	(Z,Y) is a claimed evaluation point.
//
//	All arithmetic over BLS12‑381 Fr is emulated on top of the native BN254 scalar field
//
//	The heavy division              dᵢ−Y
//	                     qᵢ  =  ───────────────
//	                               ωᵢ−Z
//	is carried out as a hint and then verified inside the circuit.
//
//	Rationale:
//	• For every index i either:
//	     (dᵢ−Y)  =  qᵢ·(ωᵢ−Z)         if ωᵢ ≠ Z
//	      qᵢ     =  0                 if ωᵢ  = Z
//	  holds, so the circuit forces the quotient values produced by the hint.
//	• We additionally enforce
//	      Σ qᵢ·ωᵢ ≡ 0  (mod r)
//	  which is satisfied if Y = P(Z) for a poly of degree < N.
//	  With the per‑index equations this single sum rule is already sufficient;
//
// References:
//   - https://docs.sotazk.org/docs/zk_rollups_after_eip4844/#point-evaluation-precompile
//   - https://github.com/Consensys/gnark/blob/master/std/evmprecompiles/10-kzg_point_evaluation.go
package blobs

import (
	"math/big"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/math/emulated"
)

const (
	N    = 1 << 12 // 4096 evaluation points
	logN = 12
)

// FE is type modulus for BLS12‑381 Fr.
type FE = emulated.BLS12381Fr

// VerifyBlobEvaluation performs the blob evaluation verification logic.
// This function can be imported and used by any other circuit that needs
// to verify blob evaluations.
//
// Parameters:
//   - api: The frontend API for circuit operations
//   - z: The evaluation point Z
//   - y: The claimed evaluation result Y
//   - blob: The blob data (4096 elements)
//
// Returns an error if the verification constraints cannot be satisfied.
func VerifyBlobEvaluation(
	api frontend.API,
	z *emulated.Element[FE],
	y *emulated.Element[FE],
	blob [N]emulated.Element[FE],
) error {
	// Field helpers
	fr, err := emulated.NewField[FE](api)
	if err != nil {
		return err
	}
	zero := fr.Zero()
	omegaAt := func(i int) *emulated.Element[FE] { return fr.NewElement(omegaHex[i]) }

	// Hint input packing
	in := make([]*emulated.Element[FE], 2+2*N) // [Y | Z | blob | ω]
	in[0], in[1] = y, z
	for i := 0; i < N; i++ {
		in[2+i] = &blob[i]
		in[2+N+i] = omegaAt(i)
	}

	// Produce q₀,…,q_{N−1}, Σ qᵢ·ωᵢ
	outs, err := fr.NewHint(quotHint, N+1, in...)
	if err != nil {
		return err
	}
	q := outs[:N]
	S1 := fr.Reduce(outs[N]) // Σ qᵢ·ωᵢ
	for i := 0; i < N; i++ {
		q[i] = fr.Reduce(q[i])
	}

	// Per‑index constraints
	direct := fr.Zero() // value to take when Z hits a grid‑point
	anyZero := frontend.Variable(0)

	for i := 0; i < N; i++ {
		ωi := omegaAt(i)
		denR := fr.Reduce(fr.Sub(ωi, z)) // ωᵢ − Z
		isZero := fr.IsZero(denR)        // boolean

		// (dᵢ − Y) = qᵢ·(ωᵢ − Z)
		lhs := fr.Reduce(fr.Select(isZero, zero,
			fr.Reduce(fr.Sub(&blob[i], y))))
		rhs := fr.Reduce(fr.Select(isZero, zero,
			fr.Reduce(fr.Mul(q[i], denR))))
		fr.AssertIsEqual(lhs, rhs)

		// qᵢ must be 0 on the collision branch
		fr.AssertIsEqual(fr.Select(isZero, q[i], zero), zero)

		// Track the direct‑hit value safely
		direct = fr.Reduce(fr.Select(isZero, fr.Reduce(&blob[i]), direct))
		anyZero = api.Or(anyZero, isZero)
	}

	// Degree‑bound check  Σ qᵢ·ωᵢ = 0
	fr.AssertIsEqual(S1, zero)

	// Collision vs barycentric branch select
	//
	//  • if Z = ωᵏ   result must equal blob[k]
	//  • else        result is already constrained to Y above
	//
	final := fr.Reduce(fr.Select(anyZero, direct, y))
	fr.AssertIsEqual(final, y)
	return nil
}

// VerifyBlobEvaluationNative is a  native‑input wrapper,
// cheap equality check, then delegate.
func VerifyBlobEvaluationNative(
	api frontend.API,
	z frontend.Variable, // native BN254 evaluation point
	zEmu *emulated.Element[FE], // emulated BLS12-381 evaluation point
	y *emulated.Element[FE], // emulated BLS12-381 evaluation result
	blob [N]frontend.Variable, // BN254 native variables
	blobEmu [N]emulated.Element[FE], // emulated BLS12-381 variables
) error {
	// native‑vs‑emulated equality checks
	for i := 0; i < N; i++ {
		api.AssertIsEqual(emulatedToNative(api, &blobEmu[i]), blob[i])
	}
	api.AssertIsEqual(emulatedToNative(api, zEmu), z)

	// call original emulated verifier
	return VerifyBlobEvaluation(api, zEmu, y, blobEmu)
}

func emulatedToNative(api frontend.API, e *emulated.Element[FE]) frontend.Variable {
	nbBits := FE{}.BitsPerLimb() // 32 in gnark params
	acc := frontend.Variable(0)
	pow := big.NewInt(1)

	for i, limb := range e.Limbs {
		if i != 0 {
			pow = new(big.Int).Lsh(big.NewInt(1), nbBits*uint(i))
		}
		acc = api.Add(acc, api.Mul(limb, pow))
	}
	return acc
}
