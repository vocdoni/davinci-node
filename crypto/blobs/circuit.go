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
	Y               emulated.Element[emulated.BLS12381Fr]     `gnark:",public"`
	Blob            [4096]emulated.Element[sw_bls12381.ScalarField]
}

func (c *BlobEvalCircuit) Define(api frontend.API) error {
	fr, err := emulated.NewField[emulated.BLS12381Fr](api)
	if err != nil {
		return fmt.Errorf("field init: %w", err)
	}
	one := fr.One()
	zero := fr.Zero()

	omegaAt := func(i int) *emulated.Element[emulated.BLS12381Fr] {
		return fr.NewElement(omegaLimbs[i]) // limbs slice

	}
	nInverse := fr.NewElement(nInverseLimbs) // or fr.NewElement(nInvHex)

	api.Println("=== CIRCUIT DEBUG START ===")
	api.Println("Z:", c.Z.Limbs)
	api.Println("First 5 blob values:", c.Blob[0].Limbs, c.Blob[1].Limbs, c.Blob[2].Limbs, c.Blob[3].Limbs, c.Blob[4].Limbs)

	// Use pre-computed omega values from omega_table.go (now correctly generated)
	// These match the Go implementation exactly after regeneration

	//--------------------------------------------------------------------
	// 1. diffSafe[i] = z - ωᵢ  (but replaced by 1 if the diff is 0)
	//    isZero[i]   = (z == ωᵢ) flag
	//--------------------------------------------------------------------
	diffSafe := make([]*emulated.Element[sw_bls12381.ScalarField], N)
	isZero := make([]frontend.Variable, N)

	for i := 0; i < N; i++ {
		wi := omegaAt(i)
		d := fr.Sub(&c.Z, wi)                      // z − ωᵢ
		isZero[i] = fr.IsZero(d)                   // boolean: z == ωᵢ
		diffSafe[i] = fr.Select(isZero[i], one, d) // if 0, use 1 so invert works
	}

	api.Println("=== BLOCK 1: DIFFERENCES AND ZERO DETECTION ===")
	api.Println("First 5 omega values:", omegaAt(0).Limbs)
	api.Println("First 5 differences:", diffSafe[0].Limbs, diffSafe[1].Limbs, diffSafe[2].Limbs, diffSafe[3].Limbs, diffSafe[4].Limbs)
	api.Println("First 5 isZero flags:", isZero[0], isZero[1], isZero[2], isZero[3], isZero[4])

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

	api.Println("=== BLOCK 2: BATCH INVERSION ===")
	api.Println("prodAll:", prodAll.Limbs)
	api.Println("invAll (after inversion):", invAll.Limbs)
	api.Println("First 5 prefix values:", prefix[0].Limbs, prefix[1].Limbs, prefix[2].Limbs, prefix[3].Limbs, prefix[4].Limbs)
	api.Println("First 5 inv values:", inv[0].Limbs, inv[1].Limbs, inv[2].Limbs, inv[3].Limbs, inv[4].Limbs)

	//--------------------------------------------------------------------
	// 3. sum = Σ dᵢ·ωᵢ / (z−ωᵢ)
	//    Skip the term if denominator was zero (handled via isZero flag).
	//--------------------------------------------------------------------
	sum := fr.Zero()
	var firstTerms [5]*emulated.Element[sw_bls12381.ScalarField]
	for i := 0; i < N; i++ {
		w := omegaAt(i)
		term := fr.Mul(fr.Mul(&c.Blob[i], w), inv[i])
		term = fr.Select(isZero[i], zero, term) // zero-out if z==ωᵢ
		if i < 5 {
			firstTerms[i] = term
		}
		sum = fr.Add(sum, term)
	}

	api.Println("=== BLOCK 3: SUMMATION ===")
	api.Println("First 5 terms:", firstTerms[0].Limbs, firstTerms[1].Limbs, firstTerms[2].Limbs, firstTerms[3].Limbs, firstTerms[4].Limbs)
	api.Println("Sum:", sum.Limbs)

	//--------------------------------------------------------------------
	// 4. factor = (z^4096 − 1) · 4096⁻¹
	//--------------------------------------------------------------------
	zPow := c.Z
	for k := 0; k < logN; k++ { // 12 squarings → z^(2^12) = z^4096
		zPow = *fr.Mul(&zPow, &zPow)
	}

	// Use pre-computed nInverse from omega_table.go (matches Go implementation)
	factor := fr.Mul(fr.Sub(&zPow, one), nInverse)

	// barycentric value
	yBary := fr.Mul(factor, sum)

	api.Println("=== BLOCK 4: FACTOR COMPUTATION ===")
	api.Println("z^4096:", zPow.Limbs)
	api.Println("nInverse:", nInverse.Limbs)
	api.Println("factor:", factor.Limbs)
	api.Println("yBary (factor * sum):", yBary.Limbs)

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

	api.Println("=== BLOCK 5: FINAL SELECTION ===")
	api.Println("direct:", direct.Limbs)
	api.Println("anyZero:", anyZero)
	api.Println("final:", final.Limbs)
	api.Println("Expected Y:", c.Y.Limbs)

	//--------------------------------------------------------------------
	// 6. Enforce Y == final (compare limbs directly to avoid field assertion issues)
	//--------------------------------------------------------------------
	// Compare each limb individually
	for i := range c.Y.Limbs {
		api.Println("Comparing Y limb", i, ":", c.Y.Limbs[i], "==", final.Limbs[i])
		api.AssertIsEqual(final.Limbs[i], c.Y.Limbs[i])
	}

	return nil
}
