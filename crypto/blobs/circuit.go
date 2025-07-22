// Package blobs implements a circuit that evaluates a blob polynomial at a given point
// using barycentric evaluation. This circuit is designed to work with the EVM precompile
// 0x0A for KZG verification in a split-verification architecture.
package blobs

import (
	"fmt"

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
func (c *BlobEvalCircuit) Define(api frontend.API) error {
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

	// Pre-compute sum accumulator
	sum := frAPI.Zero()

	// Pre-allocate value slices for batch inversion (not pointer slices)
	diff := make([]emulated.Element[sw_bls12381.ScalarField], 4096)
	prefix := make([]emulated.Element[sw_bls12381.ScalarField], 4096)

	// --- Forward scan: compute all differences and build prefix products
	prod := frAPI.One()
	for i := range 4096 {
		// Compute (z - ω^i)
		diff[i] = *frAPI.Sub(&c.Z, &omega[i])

		// Store prefix product (copy value, not pointer)
		prefix[i] = *prod

		// Update product
		prod = frAPI.Mul(prod, &diff[i])
	}

	// Single inverse of the final product
	invAll := frAPI.Inverse(prod)

	// --- Backward scan: compute individual inverses and accumulate sum
	for i := 4096 - 1; i >= 0; i-- {
		// Compute 1 / (z - ω^i) using batch inversion trick
		invDiff := frAPI.Mul(invAll, &prefix[i])
		invAll = frAPI.Mul(invAll, &diff[i])

		// Accumulate blob[i] * ω^i / (z - ω^i)
		// This matches the c-kzg-4844 implementation exactly
		term := frAPI.Mul(&c.Blob[i], &omega[i])
		term = frAPI.Mul(term, invDiff)
		sum = frAPI.Add(sum, term)
	}

	// Compute z^4096 efficiently using repeated squaring
	// 4096 = 2^12, so we need 12 squarings
	// Start with z as the initial value
	zPowN := &c.Z
	for range 12 {
		zPowN = frAPI.Mul(zPowN, zPowN)
	}

	// Compute (z^4096 - 1)
	one := frAPI.One()
	zPowNMinus1 := frAPI.Sub(zPowN, one)

	// Compute (z^4096 - 1) / 4096
	factor := frAPI.Mul(zPowNMinus1, &nInverse)

	// Compute y = factor * sum
	// This is the final step from c-kzg-4844: blst_fr_mul(out, out, &tmp)
	yCalc := frAPI.Mul(factor, sum)

	// ---------- Enforce equality ----------
	frAPI.AssertIsEqual(yCalc, &c.Y)

	// Note: CommitmentLimbs are public inputs but don't need constraints within the circuit.
	// They are part of the witness that the verifier provides, and will be used
	// externally to verify the KZG opening via EVM precompile 0x0A.

	return nil
}
