package blobs

import (
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/test"
	gethkzg "github.com/ethereum/go-ethereum/crypto/kzg4844"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/hash/poseidon"
	"github.com/vocdoni/davinci-node/util"
)

// TestBlobEvaluationCircuitWithActualData tests ONLY barycentric evaluation with actual blob data.
func TestBlobEvaluationCircuitWithActualData(t *testing.T) {
	c := qt.New(t)

	blob, err := GetBlobData1()
	c.Assert(err, qt.IsNil)

	// Compute commitment first, then evaluation point
	commitment, err := gethkzg.BlobToCommitment(blob)
	c.Assert(err, qt.IsNil)

	processID := util.RandomBytes(31)
	rootHashBefore := util.RandomBytes(31)
	z, err := ComputeEvaluationPoint(new(big.Int).SetBytes(processID), new(big.Int).SetBytes(rootHashBefore), commitment)
	c.Assert(err, qt.IsNil)

	// Compute KZG proof to get Y value
	_, claim, err := gethkzg.ComputeProof(blob, BigIntToPoint(z))
	c.Assert(err, qt.IsNil)
	y := new(big.Int).SetBytes(claim[:])

	// Create witness for barycentric evaluation only (no KZG verification)
	witness := blobEvalCircuitBarycentricOnly{
		Z: emulated.ValueOf[FE](z),
		Y: emulated.ValueOf[FE](y),
	}

	// Fill blob data
	for i := range 4096 {
		cell := new(big.Int).SetBytes(blob[i*32 : (i+1)*32])
		witness.Blob[i] = emulated.ValueOf[FE](cell)
	}

	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&blobEvalCircuitBarycentricOnly{}, &witness,
		test.WithCurves(circuits.StateTransitionCurve), test.WithBackends(backend.GROTH16))
}

// TestProgressiveElementsNative tests the circuit with increasing number of elements
func TestBlobEvaluationCircuitProgressive(t *testing.T) {
	std.RegisterHints()
	c := qt.New(t)

	testCounts := []int{10, 100}

	for _, count := range testCounts {
		fmt.Printf("\n=== Testing with %d elements ===\n", count)

		// Create blob with 'count' elements
		blob := &gethkzg.Blob{}
		for i := range count {
			val := big.NewInt(int64(i + 1))
			valHash, err := poseidon.MultiPoseidon(val) // Ensure the cell is processed by Poseidon
			c.Assert(err, qt.IsNil)
			valHash.FillBytes(blob[i*32 : (i+1)*32])
		}

		// Compute commitment first
		commitment, err := gethkzg.BlobToCommitment(blob)
		c.Assert(err, qt.IsNil)

		// Compute evaluation point
		processID := util.RandomBytes(31)
		rootHashBefore := util.RandomBytes(31)
		z, err := ComputeEvaluationPoint(new(big.Int).SetBytes(processID), new(big.Int).SetBytes(rootHashBefore), commitment)
		c.Assert(err, qt.IsNil)

		// Compute KZG proof
		proof, claim, err := gethkzg.ComputeProof(blob, BigIntToPoint(z))
		c.Assert(err, qt.IsNil)

		// Convert geth-kzg types to circuit inputs using the helper function
		commitmentPoint, proofPoint, y, err := KZGToCircuitInputs(commitment, proof, claim)
		c.Assert(err, qt.IsNil)

		// Create witness
		witness := blobEvalCircuitBN254{
			Z:          z,
			Y:          emulated.ValueOf[FE](y),
			Commitment: commitmentPoint,
			Proof:      proofPoint,
		}

		// Fill blob data
		for i := range 4096 {
			cell := new(big.Int).SetBytes(blob[i*32 : (i+1)*32])
			witness.Blob[i] = cell
		}

		// Test with IsSolved
		assert := test.NewAssert(t)
		assert.SolvingSucceeded(&blobEvalCircuitBN254{}, &witness,
			test.WithCurves(circuits.StateTransitionCurve), test.WithBackends(backend.GROTH16))
	}
}

func TestBlobEvaluationCircuitFullProving(t *testing.T) {
	c := qt.New(t)

	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == "false" {
		t.Skip("skipping circuit tests...")
	}

	// Create test data
	blob := &gethkzg.Blob{}
	for i := range 50 {
		val := big.NewInt(int64(i + 1))
		valHash, err := poseidon.MultiPoseidon(val) // Ensure the cell is processed by Poseidon
		c.Assert(err, qt.IsNil)
		valHash.FillBytes(blob[i*32 : (i+1)*32])
	}

	// Compute commitment first
	commitment, err := gethkzg.BlobToCommitment(blob)
	c.Assert(err, qt.IsNil)

	processID := util.RandomBytes(31)
	rootHashBefore := util.RandomBytes(31)
	z, err := ComputeEvaluationPoint(new(big.Int).SetBytes(processID), new(big.Int).SetBytes(rootHashBefore), commitment)
	c.Assert(err, qt.IsNil)

	// Compute KZG proof
	proof, claim, err := gethkzg.ComputeProof(blob, BigIntToPoint(z))
	c.Assert(err, qt.IsNil)

	// Convert geth-kzg types to circuit inputs using the helper function
	commitmentPoint, proofPoint, y, err := KZGToCircuitInputs(commitment, proof, claim)
	c.Assert(err, qt.IsNil)

	// Create witness
	witness := blobEvalCircuitBN254{
		Z:          z,
		Y:          emulated.ValueOf[FE](y),
		Commitment: commitmentPoint,
		Proof:      proofPoint,
	}

	// Fill blob data from gethkzg.Blob
	for i := range 4096 {
		cell := new(big.Int).SetBytes(blob[i*32 : (i+1)*32])
		witness.Blob[i] = cell
	}

	// Compile circuit
	var circuit blobEvalCircuitBN254
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)
	c.Assert(err, qt.IsNil)
	fmt.Printf("Circuit compiled with %d constraints\n", ccs.GetNbConstraints())

	// Run trusted setup
	pk, vk, err := groth16.Setup(ccs)
	c.Assert(err, qt.IsNil)

	// Create proof
	fullWitness, err := frontend.NewWitness(&witness, ecc.BN254.ScalarField())
	c.Assert(err, qt.IsNil)
	proof16, err := groth16.Prove(ccs, pk, fullWitness)
	c.Assert(err, qt.IsNil)

	// Verify proof
	publicWitness := blobEvalCircuitBN254{
		Z:          witness.Z,
		Y:          witness.Y,
		Commitment: witness.Commitment,
		Proof:      witness.Proof,
	}
	publicW, err := frontend.NewWitness(&publicWitness, ecc.BN254.ScalarField(), frontend.PublicOnly())
	c.Assert(err, qt.IsNil)
	err = groth16.Verify(proof16, vk, publicW)
	c.Assert(err, qt.IsNil)
}

// TestBlobEvalDataTransform tests that BlobEvalData.Set() correctly transforms
// geth-kzg data into Gnark circuit-compatible format.
func TestBlobEvalDataTransform(t *testing.T) {
	c := qt.New(t)

	// Create test blob with deterministic data
	blob := &gethkzg.Blob{}
	for i := range 50 {
		val := big.NewInt(int64(i + 1))
		valHash, err := poseidon.MultiPoseidon(val)
		c.Assert(err, qt.IsNil)
		valHash.FillBytes(blob[i*32 : (i+1)*32])
	}

	// Compute commitment first, then evaluation point
	commitment, err := gethkzg.BlobToCommitment(blob)
	c.Assert(err, qt.IsNil)

	processID := util.RandomBytes(31)
	rootHashBefore := util.RandomBytes(31)
	z, err := ComputeEvaluationPoint(new(big.Int).SetBytes(processID), new(big.Int).SetBytes(rootHashBefore), commitment)
	c.Assert(err, qt.IsNil)

	// Initialize BlobEvalData and perform transformation
	blobData := &BlobEvalData{}
	_, err = blobData.Set(blob, z)
	c.Assert(err, qt.IsNil)

	// Verify Z transformation
	c.Assert(blobData.Z.Cmp(z), qt.Equals, 0, qt.Commentf("Z should match evaluation point"))
	// ForGnark.Z is a frontend.Variable interface wrapping the same z value - verified in circuit test below

	// Verify commitment was computed
	c.Assert(len(blobData.Commitment), qt.Equals, 48, qt.Commentf("Commitment should be 48 bytes"))

	// Verify opening proof was computed
	c.Assert(len(blobData.OpeningProof), qt.Equals, 48, qt.Commentf("OpeningProof should be 48 bytes"))

	// Verify Y was set
	c.Assert(blobData.Y, qt.Not(qt.IsNil), qt.Commentf("Y should be set"))

	// Verify Y limbs were computed (4 limbs for BN254 compatibility)
	for i, limb := range blobData.Ylimbs {
		c.Assert(limb, qt.Not(qt.IsNil), qt.Commentf("Ylimb[%d] should be set", i))
	}

	// Verify blob cells were transformed (spot check a few cells)
	// Full validation happens in the circuit test below
	for i := 0; i < 5; i++ {
		c.Assert(blobData.ForGnark.Blob[i], qt.Not(qt.IsNil),
			qt.Commentf("ForGnark.Blob[%d] should be set", i))
	}

	// Verify the transformation can be used in a circuit
	// Create witness using the transformed data
	witness := blobEvalCircuitBN254{
		Z:          blobData.ForGnark.Z,
		Y:          blobData.ForGnark.Y,
		Blob:       blobData.ForGnark.Blob,
		Commitment: blobData.ForGnark.Commitment,
		Proof:      blobData.ForGnark.OpeningProof,
	}

	// Test that the circuit accepts this witness
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&blobEvalCircuitBN254{}, &witness,
		test.WithCurves(circuits.StateTransitionCurve), test.WithBackends(backend.GROTH16))
}

// TestBlobEvalDataTransformWithActualData tests BlobEvalData.Set() with embedded test data.
func TestBlobEvalDataTransformWithActualData(t *testing.T) {
	c := qt.New(t)

	// Use actual embedded blob data
	blob, err := GetBlobData1()
	c.Assert(err, qt.IsNil)

	// Compute commitment first, then evaluation point
	commitment, err := gethkzg.BlobToCommitment(blob)
	c.Assert(err, qt.IsNil)

	processID := util.RandomBytes(31)
	rootHashBefore := util.RandomBytes(31)
	z, err := ComputeEvaluationPoint(new(big.Int).SetBytes(processID), new(big.Int).SetBytes(rootHashBefore), commitment)
	c.Assert(err, qt.IsNil)

	// Initialize BlobEvalData and perform transformation
	blobData := &BlobEvalData{}
	_, err = blobData.Set(blob, z)
	c.Assert(err, qt.IsNil)

	// Verify basic transformations
	c.Assert(blobData.Z.Cmp(z), qt.Equals, 0, qt.Commentf("Z should match evaluation point"))
	c.Assert(blobData.Y, qt.Not(qt.IsNil))
	c.Assert(len(blobData.Commitment), qt.Equals, 48)
	c.Assert(len(blobData.OpeningProof), qt.Equals, 48)

	// Verify cell proofs were computed (EIP-7594)
	c.Assert(len(blobData.CellProofs), qt.Equals, gethkzg.CellProofsPerBlob,
		qt.Commentf("Should have %d cell proofs", gethkzg.CellProofsPerBlob))

	// Verify TxSidecar can be created
	sidecar := blobData.TxSidecar()
	c.Assert(sidecar, qt.Not(qt.IsNil))
	c.Assert(len(sidecar.Blobs), qt.Equals, 1)
	c.Assert(len(sidecar.Commitments), qt.Equals, 1)

	// Verify blob hash
	hash := blobData.HashV1()
	c.Assert(len(hash), qt.Equals, 32)
}
