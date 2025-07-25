// Package blobs implements a Gnark circuit that proves
//
//	y = P(z)
//
// where P is the polynomial encoded in a Proto-Danksharding blob (4096 evaluations).
//
// The circuit uses the exact barycentric evaluation formula implemented by go-eth-kzg / c-kzg-4844:
//
//	y = (z^4096 - 1) / 4096 * Σᵢ dᵢ * ωᵢ / (z - ωᵢ)
//
// with the standard early-exit rule: if z equals one of the domain points ωᵏ,
// then y = dᵏ (no barycentric sum needed).
//
// Implementation details:
//   - All non‑native arithmetic is done in BLS12-381 Fr emulated over the native curve.
//   - Constants (ωᵢ, 4096⁻¹) are injected with fr.NewElement(...). This ensures Gnark marks them
//     as internal constants with correct limb widths for the current native curve.
//   - We never mutate (overwrite) an Element that has been used as an operand to a Mul/Inverse.
//   - Overflow control: after long addition chains or any Select, call fr.Reduce to keep
//     limbs within expected bounds before the next Mul.
//   - Batch inversion uses the standard prefix-product trick.
//
// Regenerating the omega table:
//
//	See scripts/gen_omega_hex.go. It outputs omegaHex[4096]string and nInvHex.
//	Commit the generated file to the repo (e.g. blobs/omega_hex.go).
package blobs

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/math/emulated"
)

const (
	logN = 12
	N    = 1 << logN // 4096
)

// FE aliases the emulated BLS12-381 scalar field parameters.
type FE = emulated.BLS12381Fr

// BlobEvalCircuit – public inputs: commitment limbs (3x uint384), z, y; private: the 4096 blob cells.
type BlobEvalCircuit struct {
	CommitmentLimbs [3]frontend.Variable `gnark:",public"`
	Z               emulated.Element[FE] `gnark:",public"`
	Y               emulated.Element[FE] `gnark:",public"`
	Blob            [N]emulated.Element[FE]
}

func (c *BlobEvalCircuit) Define(api frontend.API) error {
	// Field helper for BLS12-381 Fr emulated arithmetic.
	fr, err := emulated.NewField[FE](api)
	if err != nil {
		return err
	}
	one, zero := fr.One(), fr.Zero()

	// Helpers to load constants as proper field elements.
	omegaAt := func(i int) *emulated.Element[FE] { return fr.NewElement(omegaHex[i]) }
	nInv := fr.NewElement(nInvHex)

	// 1. diffSafe[i] = z - ωᵢ   (replace 0 by 1)   and isZero[i] flag
	diffSafe := make([]*emulated.Element[FE], N)
	isZero := make([]frontend.Variable, N)

	for i := 0; i < N; i++ {
		wi := omegaAt(i)
		d := fr.Sub(&c.Z, wi)
		isZero[i] = fr.IsZero(d)
		diffSafe[i] = fr.Reduce(fr.Select(isZero[i], one, d))
	}

	// 2. Batch invert all diffSafe[i] with the prefix-product trick
	prefix := make([]*emulated.Element[FE], N)
	prefix[0] = one
	for i := 1; i < N; i++ {
		prefix[i] = fr.Reduce(fr.Mul(prefix[i-1], diffSafe[i-1]))
	}
	prodAll := fr.Reduce(fr.Mul(prefix[N-1], diffSafe[N-1]))
	invAll := fr.Inverse(prodAll) // result is reduced

	inv := make([]*emulated.Element[FE], N)
	cur := invAll
	for i := N - 1; i >= 0; i-- {
		inv[i] = fr.Reduce(fr.Mul(cur, prefix[i]))
		if i > 0 {
			cur = fr.Reduce(fr.Mul(cur, diffSafe[i]))
		}
		// Optional: sanity relation (inv[i] * diffSafe[i] == 1) skipped to save constraints.
	}

	// 3. sum = Σ dᵢ·ωᵢ / (z−ωᵢ)
	sum := fr.Zero()
	const chunk = 64 // periodic reduction to keep overflow small
	for i := 0; i < N; i++ {
		wi := omegaAt(i)
		term1 := fr.Reduce(fr.Mul(&c.Blob[i], wi))
		term2 := fr.Reduce(fr.Mul(term1, inv[i]))
		term := fr.Reduce(fr.Select(isZero[i], zero, term2))
		sum = fr.Add(sum, term)
		if (i+1)%chunk == 0 {
			sum = fr.Reduce(sum)
		}
	}
	sum = fr.Reduce(sum)

	// 4. factor = (z^4096 − 1) · 4096⁻¹
	zPowPtr := &c.Z
	for k := 0; k < logN; k++ {
		next := fr.Reduce(fr.Mul(zPowPtr, zPowPtr))
		zPowPtr = next
	}
	zPow := zPowPtr
	factor := fr.Reduce(fr.Mul(fr.Sub(zPow, one), nInv))

	// barycentric value
	yBary := fr.Reduce(fr.Mul(factor, sum))

	// 5. Direct hit: if z == ωᵏ for some k, return dᵏ instead
	direct := fr.Zero()
	for i := 0; i < N; i++ {
		direct = fr.Select(isZero[i], &c.Blob[i], direct)
	}
	direct = fr.Reduce(direct)

	anyZero := isZero[0]
	for i := 1; i < N; i++ {
		anyZero = api.Or(anyZero, isZero[i])
	}
	final := fr.Reduce(fr.Select(anyZero, direct, yBary))

	// 6. Enforce Y == final
	fr.AssertIsEqual(final, &c.Y)
	return nil
}
