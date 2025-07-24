// Package blobs implements a gnark circuit that proves
//
//	y = P(z)
//
// where P is the polynomial encoded in a Proto‑Danksharding blob.
package blobs

import (
	"fmt"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bls12381"
	"github.com/consensys/gnark/std/math/emulated"
)

const (
	logN = 12
	N    = 1 << logN // 4096
)

// BlobEvalCircuit – public inputs: commitment, z, y; private: the 4 096 cells.
type BlobEvalCircuit struct {
	CommitmentLimbs [3]frontend.Variable                      `gnark:",public"`
	Z               emulated.Element[sw_bls12381.ScalarField] `gnark:",public"`
	Y               emulated.Element[sw_bls12381.ScalarField] `gnark:",public"`
	Blob            [4096]emulated.Element[sw_bls12381.ScalarField]
}

func (c *BlobEvalCircuit) Define(api frontend.API) error {
	fr, err := emulated.NewField[emulated.BLS12381Fr](api)
	if err != nil {
		return fmt.Errorf("field init: %w", err)
	}
	one := fr.One()
	zero := fr.Zero()

	//--------------------------------------------------------------------
	// 1. diffSafe[i] = z - ωᵢ  (but replaced by 1 if the diff is 0)
	//    isZero[i]   = (z == ωᵢ) flag
	//--------------------------------------------------------------------
	diffSafe := make([]*emulated.Element[sw_bls12381.ScalarField], N)
	isZero := make([]frontend.Variable, N)

	for i := 0; i < N; i++ {
		d := fr.Sub(&c.Z, &omega[i])               // z − ωᵢ
		isZero[i] = fr.IsZero(d)                   // boolean: z == ωᵢ
		diffSafe[i] = fr.Select(isZero[i], one, d) // if 0, use 1 so invert works
	}

	//--------------------------------------------------------------------
	// 2. Batch invert all diffSafe[i]
	//    Standard prefix-product trick:
	//    prefix[i] = Π_{j < i} diffSafe[j]
	//    invAll    = (Π diffSafe[j])⁻¹
	//    inv[i]    = invAll * prefix[i]
	//--------------------------------------------------------------------
	prefix := make([]*emulated.Element[sw_bls12381.ScalarField], N)
	prefix[0] = one
	for i := 1; i < N; i++ {
		prefix[i] = fr.Mul(prefix[i-1], diffSafe[i-1])
	}
	// product of all diffs
	prodAll := fr.Mul(prefix[N-1], diffSafe[N-1])
	invAll := fr.Inverse(prodAll)

	inv := make([]*emulated.Element[sw_bls12381.ScalarField], N)
	for i := N - 1; i >= 0; i-- {
		inv[i] = fr.Mul(invAll, prefix[i])
		if i > 0 {
			invAll = fr.Mul(invAll, diffSafe[i])
		}
	}

	//--------------------------------------------------------------------
	// 3. sum = Σ dᵢ·ωᵢ / (z−ωᵢ)
	//    Skip the term if denominator was zero (handled via isZero flag).
	//--------------------------------------------------------------------
	sum := fr.Zero()
	for i := 0; i < N; i++ {
		term := fr.Mul(fr.Mul(&c.Blob[i], &omega[i]), inv[i])
		term = fr.Select(isZero[i], zero, term) // zero-out if z==ωᵢ
		sum = fr.Add(sum, term)
	}

	//--------------------------------------------------------------------
	// 4. factor = (z^4096 − 1) · 4096⁻¹
	//--------------------------------------------------------------------
	zPow := c.Z
	for k := 0; k < logN; k++ { // 12 squarings → z^(2^12) = z^4096
		zPow = *fr.Mul(&zPow, &zPow)
	}
	factor := fr.Mul(fr.Sub(&zPow, one), &nInverse)

	// barycentric value
	yBary := fr.Mul(factor, sum)

	//--------------------------------------------------------------------
	// 5. If z == ωᵏ for some k, return blob[k] instead of barycentric value
	//--------------------------------------------------------------------
	direct := fr.Zero()
	for i := 0; i < N; i++ {
		direct = fr.Select(isZero[i], &c.Blob[i], direct)
	}

	// anyZero = OR over all isZero[i]
	anyZero := isZero[0]
	for i := 1; i < N; i++ {
		anyZero = api.Or(anyZero, isZero[i])
	}

	final := fr.Select(anyZero, direct, yBary)

	api.Println("Barycentric evaluation:", final.Limbs)

	//--------------------------------------------------------------------
	// 6. Enforce Y == final
	//--------------------------------------------------------------------
	fr.AssertIsEqual(final, &c.Y)
	return nil
}
