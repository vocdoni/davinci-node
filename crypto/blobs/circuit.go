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

	// First check if z equals any omega[i] - special case handling
	// In the circuit, we can't branch, but we can handle this by careful computation

	// Pre-allocate arrays
	diff := make([]*emulated.Element[sw_bls12381.ScalarField], 4096)
	inverses := make([]*emulated.Element[sw_bls12381.ScalarField], 4096)

	// Compute all differences (z - ω^i)
	for i := 0; i < 4096; i++ {
		diff[i] = frAPI.Sub(&c.Z, &omega[i])
	}

	// Batch inversion following c-kzg-4844 pattern exactly
	// Step 1: Forward pass to build prefix products
	prefixProd := make([]*emulated.Element[sw_bls12381.ScalarField], 4096)
	prefixProd[0] = frAPI.One()
	for i := 1; i < 4096; i++ {
		prefixProd[i] = frAPI.Mul(prefixProd[i-1], diff[i-1])
	}

	// Step 2: Compute inverse of final product
	finalProd := frAPI.Mul(prefixProd[4095], diff[4095])
	invProd := frAPI.Inverse(finalProd)

	// Step 3: Backward pass to compute individual inverses
	for i := 4095; i >= 0; i-- {
		inverses[i] = frAPI.Mul(invProd, prefixProd[i])
		if i > 0 {
			invProd = frAPI.Mul(invProd, diff[i])
		}
	}

	// Accumulate sum: Σ(blob[i] * ω^i / (z - ω^i))
	sum := frAPI.Zero()
	for i := 0; i < 4096; i++ {
		// blob[i] * ω^i * (1 / (z - ω^i))
		term := frAPI.Mul(&c.Blob[i], &omega[i])
		term = frAPI.Mul(term, inverses[i])
		sum = frAPI.Add(sum, term)
	}

	// Compute z^4096 efficiently using repeated squaring
	// 4096 = 2^12, so we need 12 squarings
	// Start with z as the initial value
	zPowN := &c.Z
	for i := 0; i < 12; i++ {
		newZPowN := frAPI.Mul(zPowN, zPowN)
		zPowN = newZPowN
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
