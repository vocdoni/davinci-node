package blobs

import (
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bls12381"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/test"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
)

// TestBarycentricFormula tests the barycentric evaluation formula outside the circuit
func TestBarycentricFormula(t *testing.T) {
	c := qt.New(t)

	// Create a simple blob
	blob := &kzg4844.Blob{}
	blob[31] = 42 // blob[0] = 42

	// Use the actual z from the failing test
	z, err := ComputeEvaluationPoint(big.NewInt(123), big.NewInt(456), 1, blob)
	c.Assert(err, qt.IsNil)
	fmt.Printf("Using evaluation point z: %s\n", z.String())

	// Compute expected y using KZG
	_, claim, err := ComputeProof(blob, z)
	c.Assert(err, qt.IsNil)
	expectedY := new(big.Int).SetBytes(claim[:])

	// Now compute y using barycentric formula
	mod := bls12381.ID.ScalarField()

	// Get the 4096-th root of unity
	rMinus1 := new(big.Int).Sub(mod, big.NewInt(1))
	generator := big.NewInt(5)
	exponent := new(big.Int).Div(rMinus1, big.NewInt(4096))
	omega := new(big.Int).Exp(generator, exponent, mod)

	// Generate all omega values
	omegas := make([]*big.Int, 4096)
	omegas[0] = big.NewInt(1)
	for i := 1; i < 4096; i++ {
		omegas[i] = new(big.Int).Mul(omegas[i-1], omega)
		omegas[i].Mod(omegas[i], mod)
	}

	// Convert blob to field elements
	blobElements := make([]*big.Int, 4096)
	for i := range 4096 {
		blobElements[i] = new(big.Int).SetBytes(blob[i*32 : (i+1)*32])
	}

	// Compute barycentric evaluation using the exact formula from c-kzg-4844
	// Formula from c-kzg-4844/src/eip4844/eip4844.c:evaluate_polynomial_in_evaluation_form
	// y = (z^n - 1) / n * Σ(blob[i] * ω^i / (z - ω^i))
	//
	// Note: This is NOT the normalized barycentric formula. KZG uses the simpler
	// form without denominator normalization.

	sum := big.NewInt(0)

	for i := range 4096 {
		// Compute (z - ω^i)
		diff := new(big.Int).Sub(z, omegas[i])
		diff.Mod(diff, mod)

		// Skip if z == ω^i to avoid division by zero
		if diff.Sign() == 0 {
			continue
		}

		// Compute 1 / (z - ω^i)
		invDiff := new(big.Int).ModInverse(diff, mod)

		// sum += blob[i] * ω^i / (z - ω^i)
		// This matches the c-kzg-4844 implementation exactly:
		// blst_fr_mul(&tmp, &inverses[i], &brp_roots_of_unity[i]);
		// blst_fr_mul(&tmp, &tmp, &poly[i]);
		// blst_fr_add(out, out, &tmp);
		term := new(big.Int).Mul(blobElements[i], omegas[i])
		term.Mul(term, invDiff)
		term.Mod(term, mod)
		sum.Add(sum, term)
		sum.Mod(sum, mod)
	}

	// Compute z^4096 - 1
	zPowN := new(big.Int).Exp(z, big.NewInt(4096), mod)
	zPowNMinus1 := new(big.Int).Sub(zPowN, big.NewInt(1))
	zPowNMinus1.Mod(zPowNMinus1, mod)

	// Compute 1/4096
	nInv := new(big.Int).ModInverse(big.NewInt(4096), mod)

	// Compute (z^4096 - 1) / 4096
	factor := new(big.Int).Mul(zPowNMinus1, nInv)
	factor.Mod(factor, mod)

	// Compute y = factor * sum
	// This matches the final step in c-kzg-4844: blst_fr_mul(out, out, &tmp)
	computedY := new(big.Int).Mul(factor, sum)
	computedY.Mod(computedY, mod)

	c.Assert(expectedY.Cmp(computedY), qt.Equals, 0, qt.Commentf("Expected %s, got %s", expectedY.String(), computedY.String()))
}

// TestOmegaRoots verifies that our omega values match what KZG expects
func TestOmegaRoots(t *testing.T) {
	c := qt.New(t)

	// Get modulus
	mod := bls12381.ID.ScalarField()

	// Generate the primitive root of unity for 4096 using generator 5
	rMinus1 := new(big.Int).Sub(mod, big.NewInt(1))
	generator := big.NewInt(5)
	exponent := new(big.Int).Div(rMinus1, big.NewInt(4096))
	primitiveRoot := new(big.Int).Exp(generator, exponent, mod)

	// Compute omega^4096 to verify it's a 4096-th root
	omega4096 := new(big.Int).Exp(primitiveRoot, big.NewInt(4096), mod)
	c.Assert(omega4096.Cmp(big.NewInt(1)), qt.Equals, 0, qt.Commentf("omega^4096 should be 1, got %s", omega4096.String()))

	// Test if our omega is primitive (not a lower order root)
	omega2048 := new(big.Int).Exp(primitiveRoot, big.NewInt(2048), mod)
	c.Assert(omega2048.Cmp(big.NewInt(1)), qt.Not(qt.Equals), 0, qt.Commentf("omega^2048 should not be 1 (should be primitive)"))

	// Create a simple blob and test with KZG
	blob := &kzg4844.Blob{}
	blob[31] = 1 // blob[0] = 1

	// Test at omega itself
	_, claim, err := ComputeProof(blob, primitiveRoot)
	c.Assert(err, qt.IsNil)
	y := new(big.Int).SetBytes(claim[:])
	fmt.Printf("\nKZG evaluation at omega: %s\n", y.String())
	// At a root of unity, p(omega) should equal blob[1] for this simple case
}

// TestProgressiveElements tests the circuit with increasing number of elements
func TestProgressiveElements(t *testing.T) {
	c := qt.New(t)

	testCounts := []int{1, 5, 20, 100}

	for _, count := range testCounts {
		fmt.Printf("\n=== Testing with %d elements ===\n", count)

		// Create blob with 'count' elements
		blob := &kzg4844.Blob{}
		for i := range count {
			val := big.NewInt(int64(i + 1))
			val.FillBytes(blob[i*32 : (i+1)*32])
		}

		// Compute commitment
		commitmentBytes, err := BlobToCommitment(blob)
		c.Assert(err, qt.IsNil)

		// Compute evaluation point
		z, err := ComputeEvaluationPoint(big.NewInt(123), big.NewInt(456), 1, blob)
		c.Assert(err, qt.IsNil)

		// Compute KZG proof
		_, claim, err := ComputeProof(blob, z)
		c.Assert(err, qt.IsNil)
		y := new(big.Int).SetBytes(claim[:])

		// Create witness
		commitmentLimbs := splitIntoLimbs(commitmentBytes[:], 3)
		witness := BlobEvalCircuit{
			CommitmentLimbs: [3]frontend.Variable{commitmentLimbs[0], commitmentLimbs[1], commitmentLimbs[2]},
			Z:               emulated.ValueOf[sw_bls12381.ScalarField](z),
			Y:               emulated.ValueOf[sw_bls12381.ScalarField](y),
		}

		// Fill blob data
		for i := range 4096 {
			cell := new(big.Int).SetBytes(blob[i*32 : (i+1)*32])
			witness.Blob[i] = emulated.ValueOf[sw_bls12381.ScalarField](cell)
		}

		// Test with IsSolved
		assert := test.NewAssert(t)
		assert.SolvingSucceeded(&BlobEvalCircuit{}, &witness,
			test.WithCurves(circuits.StateTransitionCurve), test.WithBackends(backend.GROTH16))

		fmt.Printf("Test with %d elements passed\n", count)
	}
}

func TestBlobEvalCircuitProving(t *testing.T) {
	c := qt.New(t)

	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == "false" {
		t.Skip("skipping circuit tests...")
	}

	// Create test data
	blob := &kzg4844.Blob{}
	for i := range 100 {
		val := big.NewInt(int64(i + 1))
		val.FillBytes(blob[i*32 : (i+1)*32])
	}

	// Compute commitment and proof
	commitmentBytes, err := kzg4844.BlobToCommitment(blob)
	c.Assert(err, qt.IsNil)

	z, err := ComputeEvaluationPoint(big.NewInt(123), big.NewInt(456), 1, blob)
	c.Assert(err, qt.IsNil)

	// Compute KZG proof using kzg4844
	_, claim, err := kzg4844.ComputeProof(blob, BigIntToKZGPoint(z))
	c.Assert(err, qt.IsNil)
	y := new(big.Int).SetBytes(claim[:])

	// Prepare witness with commitment limbs
	commitmentLimbs := splitIntoLimbs(commitmentBytes[:], 3)

	// Create witness
	witness := BlobEvalCircuit{
		CommitmentLimbs: [3]frontend.Variable{commitmentLimbs[0], commitmentLimbs[1], commitmentLimbs[2]},
		Z:               emulated.ValueOf[sw_bls12381.ScalarField](z),
		Y:               emulated.ValueOf[sw_bls12381.ScalarField](y),
	}

	// Fill blob data from kzg4844.Blob
	for i := 0; i < 4096; i++ {
		cell := new(big.Int).SetBytes(blob[i*32 : (i+1)*32])
		witness.Blob[i] = emulated.ValueOf[sw_bls12381.ScalarField](cell)
	}

	// Compile circuit
	var circuit BlobEvalCircuit
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)
	c.Assert(err, qt.IsNil)
	fmt.Printf("Circuit compiled with %d constraints\n", ccs.GetNbConstraints())

	// Run trusted setup
	pk, vk, err := groth16.Setup(ccs)
	c.Assert(err, qt.IsNil)

	// Create proof
	now := time.Now()
	fullWitness, err := frontend.NewWitness(&witness, ecc.BN254.ScalarField())
	c.Assert(err, qt.IsNil)
	proof16, err := groth16.Prove(ccs, pk, fullWitness)
	c.Assert(err, qt.IsNil)
	fmt.Printf("Proving took %v\n", time.Since(now))

	// Verify proof
	publicWitness := BlobEvalCircuit{
		CommitmentLimbs: witness.CommitmentLimbs,
		Z:               witness.Z,
		Y:               witness.Y,
	}
	publicW, err := frontend.NewWitness(&publicWitness, ecc.BN254.ScalarField(), frontend.PublicOnly())
	c.Assert(err, qt.IsNil)
	err = groth16.Verify(proof16, vk, publicW)
	c.Assert(err, qt.IsNil)

	fmt.Printf("\nFull proving and verification successful!\n")
}
