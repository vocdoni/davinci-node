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

// BlobEvalCircuit – public inputs: commitment, z, y; private: the 4 096 cells.
type BlobEvalCircuit struct {
	CommitmentLimbs [3]frontend.Variable                      `gnark:",public"`
	Z               emulated.Element[sw_bls12381.ScalarField] `gnark:",public"`
	Y               emulated.Element[sw_bls12381.ScalarField] `gnark:",public"`
	Blob            [4096]emulated.Element[sw_bls12381.ScalarField]
}

// Define adds constraints that certify          Y == P(Z)          where
// P is encoded in `Blob` and the evaluation uses the exact barycentric
// equation implemented by go‑eth‑kzg / c‑kzg‑4844.
func (c *BlobEvalCircuit) Define(api frontend.API) error {

	fr, err := emulated.NewField[emulated.BLS12381Fr](api)
	if err != nil {
		return fmt.Errorf("field init: %w", err)
	}
	one := fr.One()
	zero := fr.Zero()

	//--------------------------------------------------------------------
	// 1.  (z − ωᵢ)           + flag  isZeroᵢ  if z == ωᵢ
	//--------------------------------------------------------------------
	diffSafe := make([]*emulated.Element[sw_bls12381.ScalarField], 4096)
	isZero := make([]frontend.Variable, 4096)

	for i := 0; i < 4096; i++ {
		d := fr.Sub(&c.Z, &omega[i])               // z − ωᵢ
		isZero[i] = fr.IsZero(d)                   // 1 ↔ z == ωᵢ
		diffSafe[i] = fr.Select(isZero[i], one, d) // replace 0 by 1
	}

	//--------------------------------------------------------------------
	// 2.  Batch‑invert all  diffSafe[i]
	//--------------------------------------------------------------------
	prefix := make([]*emulated.Element[sw_bls12381.ScalarField], 4096)
	prefix[0] = one
	for i := 1; i < 4096; i++ {
		prefix[i] = fr.Mul(prefix[i-1], diffSafe[i-1])
	}
	globalInv := fr.Inverse(fr.Mul(prefix[4095], diffSafe[4095]))

	inv := make([]*emulated.Element[sw_bls12381.ScalarField], 4096)
	for i := 4095; i >= 0; i-- {
		inv[i] = fr.Mul(globalInv, prefix[i])
		if i > 0 {
			globalInv = fr.Mul(globalInv, diffSafe[i])
		}
	}

	//--------------------------------------------------------------------
	// 3.  Σ  dᵢ·ωᵢ / (z−ωᵢ)
	//--------------------------------------------------------------------
	sum := zero
	for i := 0; i < 4096; i++ {
		term := fr.Mul(fr.Mul(&c.Blob[i], &omega[i]), inv[i])
		term = fr.Select(isZero[i], zero, term) // skip if denominator was 0
		sum = fr.Add(sum, term)
	}

	//--------------------------------------------------------------------
	// 4.  factor = (z⁴⁰⁹⁶ − 1) · 4096⁻¹
	//--------------------------------------------------------------------
	zPow := c.Z
	for k := 0; k < 12; k++ { // 2¹² = 4096
		zPow = *fr.Mul(&zPow, &zPow)
	}
	factor := fr.Mul(fr.Sub(&zPow, one), &nInverse)

	// barycentric value
	yBary := fr.Mul(factor, sum)

	//--------------------------------------------------------------------
	// 5.  If   z == ωᵏ   return   dᵏ   instead of barycentric formula
	//--------------------------------------------------------------------
	direct := zero
	for i := 0; i < 4096; i++ {
		direct = fr.Select(isZero[i], &c.Blob[i], direct)
	}

	// anyZero = OR(isZero[0], …, isZero[4095])
	anyZero := isZero[0]
	for i := 1; i < 4096; i++ {
		anyZero = api.Or(anyZero, isZero[i])
	}
	final := fr.Select(anyZero, direct, yBary)

	//--------------------------------------------------------------------
	// 6.  Enforce equality with public Y
	//--------------------------------------------------------------------
	fr.AssertIsEqual(final, &c.Y)
	return nil
}
