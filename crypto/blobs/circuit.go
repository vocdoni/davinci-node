// Package blobs implements a circuit that evaluates a blob polynomial at a given point
// using barycentric evaluation. This circuit is designed to work with the EVM precompile
// 0x0A for KZG verification in a split-verification architecture.
package blobs

import (
	"fmt"
	"math/bits"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bls12381"
	"github.com/consensys/gnark/std/math/emulated"
)

// BlobEvalCircuit evaluates a blob polynomial at a given point using barycentric evaluation.
// The circuit proves that y = p(z) where p is the polynomial defined by the blob data.
// KZG opening verification is delegated to EVM precompile 0x0A.
type BlobEvalCircuit struct {
	// PUBLIC inputs
	CommitmentLimbs [3]frontend.Variable                      `gnark:",public"` // 3×16-byte limbs, EVM big-endian
	Z               emulated.Element[sw_bls12381.ScalarField] `gnark:",public"` // Evaluation point
	Y               emulated.Element[sw_bls12381.ScalarField] `gnark:",public"` // Claimed value

	// PRIVATE inputs
	Blob [4096]emulated.Element[sw_bls12381.ScalarField] // Full blob cells
}

// Define implements the circuit constraints using barycentric evaluation
func (c *BlobEvalCircuit) Define2(api frontend.API) error {
	frAPI, err := emulated.NewField[emulated.BLS12381Fr](api)
	if err != nil {
		return fmt.Errorf("new field: %w", err)
	}

	// ---------- Barycentric evaluation y = p(z) ----------
	// Formula from c-kzg-4844/src/eip4844/eip4844.c:evaluate_polynomial_in_evaluation_form
	// This implements the KZG polynomial evaluation used by Ethereum:
	// p(z) = (z^n - 1) / n * Σ(blob[i] * ω^i / (z - ω^i))
	// where n = 4096 and ω is a primitive n-th root of unity
	//
	// Note: This differs from the normalized barycentric formula which includes
	// a denominator sum Σ(1 / (z - ω^i)). The KZG implementation uses the simpler
	// form without denominator normalization.

	// Compute all differences (z - ω^i)
	// Note: The evaluation point z is now guaranteed to never equal any omega value
	// by the ComputeEvaluationPoint function, so we don't need special case handling
	diff := make([]*emulated.Element[sw_bls12381.ScalarField], 4096)
	for i := range 4096 {
		diff[i] = frAPI.Sub(&c.Z, &omega[i])
	}

	// Batch inversion following c-kzg-4844 pattern exactly
	// Forward pass to build prefix products
	prefixProd := make([]*emulated.Element[sw_bls12381.ScalarField], 4096)
	prefixProd[0] = frAPI.One()
	for i := 1; i < 4096; i++ {
		prefixProd[i] = frAPI.Mul(prefixProd[i-1], diff[i-1])
	}

	// Compute inverse of final product
	finalProd := frAPI.Mul(prefixProd[4095], diff[4095])
	invProd := frAPI.Inverse(finalProd)

	// Backward pass to compute individual inverses
	inverses := make([]*emulated.Element[sw_bls12381.ScalarField], 4096)
	for i := 4095; i >= 0; i-- {
		inverses[i] = frAPI.Mul(invProd, prefixProd[i])
		if i > 0 {
			invProd = frAPI.Mul(invProd, diff[i])
		}
	}

	// Accumulate sum: Σ(blob[i] * ω^i / (z - ω^i))
	sum := frAPI.Zero()
	for i := range 4096 {
		j := bitReverse12(uint32(i))
		term := frAPI.Mul(frAPI.Mul(&c.Blob[j], &omega[i]), inverses[i])
		sum = frAPI.Add(sum, term)
	}

	// Compute z^4096 efficiently using repeated squaring
	// 4096 = 2^12, so we need 12 squarings
	// Start with z as the initial value
	zPowN := c.Z
	for i := 0; i < 12; i++ {
		zPowN = *frAPI.Mul(&zPowN, &zPowN)
	}

	// Compute (z^4096 - 1)
	zPowNMinus1 := frAPI.Sub(&zPowN, frAPI.One())

	// Compute (z^4096 - 1) / 4096
	factor := frAPI.Mul(zPowNMinus1, &nInverse)

	// Compute y = factor * sum
	// This is the final step from c-kzg-4844: blst_fr_mul(out, out, &tmp)
	yCalc := frAPI.Mul(factor, sum)

	frAPI.AssertIsEqual(yCalc, &c.Y)

	return nil
}

// Define implements the circuit constraints that prove
//
//	y = p(z)
//
// where p is the degree‑4095 polynomial encoded in the 4 096 field elements
// of the blob.  The evaluation is performed with the exact barycentric
// equation used by the C‑KZG‑4844 reference implementation.
//
// The code is written so that it is *sound* for both situations
//   - z is NOT one of the 4 096 roots of unity (the typical case); or
//   - z coincides with some ωᵏ (extreme edge case).
//
// If the edge case never happens at runtime the extra selectors simply turn
// into constants and the constraint system collapses to the minimal one.
func (c *BlobEvalCircuit) Define(api frontend.API) error {
	// ---------------------------------------------------------------------
	// Helpers
	// ---------------------------------------------------------------------
	frAPI, err := emulated.NewField[emulated.BLS12381Fr](api)
	if err != nil {
		return fmt.Errorf("new field: %w", err)
	}
	one := frAPI.One()    // 1 in Fr
	zeroE := frAPI.Zero() // 0 in Fr

	// Keep track of whether z equals *any* ωᵢ and, if so, which blob value
	// must be returned directly.
	isDomainPoint := frontend.Variable(0) // boolean 0/1
	directValue := zeroE                  // will hold blob[k] if z == ωᵏ

	// We need the safe denominators and the boolean flags for later steps.
	diffSafe := make([]*emulated.Element[sw_bls12381.ScalarField], 4096)
	isZeroArr := make([]frontend.Variable, 4096)

	//----------------------------------------------------------------------
	// 1.  Build (z‑ωᵢ)  and detect the “in‑domain” edge case
	//----------------------------------------------------------------------
	for i := 0; i < 4096; i++ {
		// diff = z − ωᵢ
		diff := frAPI.Sub(&c.Z, &omega[i])

		// isZero = 1  ⇔  z == ωᵢ
		isZero := frAPI.IsZero(diff) // boolean in Fq (native)
		isZeroArr[i] = isZero

		// Update global flag   isDomainPoint = isDomainPoint  OR  isZero
		isDomainPoint = api.Select(isDomainPoint, isDomainPoint, isZero)

		// When diff == 0 we replace it with 1 so that it is always invertible.
		diffSafe[i] = frAPI.Select(isZero, one, diff)

		// If this is the matching root, remember blob[i]
		directValue = frAPI.Select(isZero, &c.Blob[i], directValue)
	}

	// ---------------------------------------------------------------------
	// 2.  Batch‑invert all denominators 1/(z‑ωᵢ)   using the classical
	//     forward / reverse trick, but with the *safe* denominators.
	// ---------------------------------------------------------------------
	prefixProd := make([]*emulated.Element[sw_bls12381.ScalarField], 4096)
	prefixProd[0] = one
	for i := 1; i < 4096; i++ {
		prefixProd[i] = frAPI.Mul(prefixProd[i-1], diffSafe[i-1])
	}

	finalProd := frAPI.Mul(prefixProd[4095], diffSafe[4095])
	invProd := frAPI.Inverse(finalProd)

	inverses := make([]*emulated.Element[sw_bls12381.ScalarField], 4096)
	for i := 4095; i >= 0; i-- {
		inverses[i] = frAPI.Mul(invProd, prefixProd[i])
		if i > 0 {
			invProd = frAPI.Mul(invProd, diffSafe[i])
		}
	}

	// ---------------------------------------------------------------------
	// 3.  Σ  blob[j] · ωᵢ / (z‑ωᵢ)
	//     (skip the i for which  z == ωᵢ  because we will use directValue)
	// ---------------------------------------------------------------------
	sum := zeroE
	for i := 0; i < 4096; i++ {
		term := frAPI.Mul(
			frAPI.Mul(&c.Blob[i], &omega[i]), // d_i · ωᵢ
			inverses[i],                      // /(z‑ωᵢ)
		)
		// If z = ωᵢ this term is ignored (directValue is used instead)
		term = frAPI.Select(isZeroArr[i], zeroE, term)
		sum = frAPI.Add(sum, term)
	}

	//----------------------------------------------------------------------
	// 4.  Multiplicative factor  (z⁴⁰⁹⁶ − 1) / 4096
	//----------------------------------------------------------------------
	// Compute z¹ … z² … z⁴ … z⁸ … z²⁴⁰⁹⁶ (12 squarings because 4096 = 2¹²)
	zPow := c.Z
	for k := 0; k < 12; k++ {
		zPow = *frAPI.Mul(&zPow, &zPow)
	}
	zPowMinus1 := frAPI.Sub(&zPow, one)
	factor := frAPI.Mul(zPowMinus1, &nInverse)

	baryValue := frAPI.Mul(factor, sum)

	//----------------------------------------------------------------------
	// 5.  Choose between the direct evaluation (edge case) and the barycentric
	//     evaluation (regular case)
	//----------------------------------------------------------------------
	yCalc := frAPI.Select(isDomainPoint, directValue, baryValue)

	//----------------------------------------------------------------------
	// 6.  Final constraint
	//----------------------------------------------------------------------
	frAPI.AssertIsEqual(yCalc, &c.Y)
	return nil
}

func bitReverse12(x uint32) uint32 {
	return bits.Reverse32(x) >> 20 // keep the low-12 reversed bits
}
