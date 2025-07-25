package blobs

import (
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/test"
	goethkzg "github.com/crate-crypto/go-eth-kzg"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/util"
)

// TestProgressiveElements tests the circuit with increasing number of elements
func TestProgressiveElements(t *testing.T) {
	std.RegisterHints()
	c := qt.New(t)

	testCounts := []int{5, 50, 100}

	for _, count := range testCounts {
		fmt.Printf("\n=== Testing with %d elements ===\n", count)

		// Create blob with 'count' elements
		blob := &goethkzg.Blob{}
		for i := 0; i < count; i++ {
			big.NewInt(int64(i + 1)).FillBytes(blob[i*32 : (i+1)*32])
		}

		// Compute commitment
		commitmentBytes, err := BlobToCommitment(blob)
		c.Assert(err, qt.IsNil)

		// Compute evaluation point
		processID := util.RandomBytes(31)
		rootHashBefore := util.RandomBytes(31)
		z, err := ComputeEvaluationPoint(new(big.Int).SetBytes(processID), new(big.Int).SetBytes(rootHashBefore), 1, blob)
		c.Assert(err, qt.IsNil)

		// Compute KZG proof
		_, claim, err := ComputeProof(blob, z)
		c.Assert(err, qt.IsNil)
		y := new(big.Int).SetBytes(claim[:])

		// Create witness
		commitmentLimbs := splitIntoLimbs(commitmentBytes[:], 3)
		witness := BlobEvalCircuit{
			CommitmentLimbs: [3]frontend.Variable{commitmentLimbs[0], commitmentLimbs[1], commitmentLimbs[2]},
			Z:               emulated.ValueOf[FE](z),
			Y:               emulated.ValueOf[FE](y),
		}

		// Fill blob data
		for i := range 4096 {
			cell := new(big.Int).SetBytes(blob[i*32 : (i+1)*32])
			witness.Blob[i] = emulated.ValueOf[FE](cell)
		}

		// Test with IsSolved
		assert := test.NewAssert(t)
		assert.SolvingSucceeded(&BlobEvalCircuit{}, &witness,
			test.WithCurves(circuits.StateTransitionCurve), test.WithBackends(backend.GROTH16))
	}
}

func TestCircuitWithActualDataBlob(t *testing.T) {
	log.Init("debug", "stdout", nil)
	c := qt.New(t)
	os.Setenv("GNARK_LOG", "debug") // or use zerolog.SetGlobalLevel

	data, err := os.ReadFile("testdata/blobdata1.txt")
	if err != nil {
		// skip test
		t.Skipf("blobdata1.txt not found, skipping test: %v", err)
	}
	blob, err := hexStrToBlob(string(data))
	c.Assert(err, qt.IsNil)

	// Compute commitment
	commitmentBytes, err := BlobToCommitment(blob)
	c.Assert(err, qt.IsNil)

	// Compute evaluation point
	processID := util.RandomBytes(31)
	rootHashBefore := util.RandomBytes(31)
	z, err := ComputeEvaluationPoint(new(big.Int).SetBytes(processID), new(big.Int).SetBytes(rootHashBefore), 1, blob)
	c.Assert(err, qt.IsNil)

	// Compute KZG proof
	_, claim, err := ComputeProof(blob, z)
	c.Assert(err, qt.IsNil)
	y := new(big.Int).SetBytes(claim[:])

	// Create witness
	commitmentLimbs := splitIntoLimbs(commitmentBytes[:], 3)
	witness := BlobEvalCircuit{
		CommitmentLimbs: [3]frontend.Variable{commitmentLimbs[0], commitmentLimbs[1], commitmentLimbs[2]},
		Z:               emulated.ValueOf[FE](z),
		Y:               emulated.ValueOf[FE](y),
	}

	// Fill blob data
	for i := range 4096 {
		cell := new(big.Int).SetBytes(blob[i*32 : (i+1)*32])
		witness.Blob[i] = emulated.ValueOf[FE](cell)
	}

	// Test with IsSolved

	assert := test.NewAssert(t)
	//assert.CheckCircuit(&BlobEvalCircuit{}, test.WithCurves(circuits.StateTransitionCurve), test.WithBackends(backend.GROTH16))

	assert.SolvingSucceeded(&BlobEvalCircuit{}, &witness,
		test.WithCurves(circuits.StateTransitionCurve), test.WithBackends(backend.GROTH16))
}

func TestCircuitFullProving(t *testing.T) {
	c := qt.New(t)

	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == "false" {
		t.Skip("skipping circuit tests...")
	}

	// Create test data
	blob := &goethkzg.Blob{}
	for i := range 50 {
		val := big.NewInt(int64(i + 1))
		val.FillBytes(blob[i*32 : (i+1)*32])
	}

	// Compute commitment and proof
	commitmentBytes, err := BlobToCommitment(blob)
	c.Assert(err, qt.IsNil)

	processID := util.RandomBytes(31)
	rootHashBefore := util.RandomBytes(31)
	z, err := ComputeEvaluationPoint(new(big.Int).SetBytes(processID), new(big.Int).SetBytes(rootHashBefore), 1, blob)
	c.Assert(err, qt.IsNil)

	// Compute KZG proof using go-eth-kzg
	_, claim, err := ComputeProof(blob, z)
	c.Assert(err, qt.IsNil)
	y := new(big.Int).SetBytes(claim[:])

	// Prepare witness with commitment limbs
	commitmentLimbs := splitIntoLimbs(commitmentBytes[:], 3)

	// Create witness
	witness := BlobEvalCircuit{
		CommitmentLimbs: [3]frontend.Variable{commitmentLimbs[0], commitmentLimbs[1], commitmentLimbs[2]},
		Z:               emulated.ValueOf[FE](z),
		Y:               emulated.ValueOf[FE](y),
	}

	// Fill blob data from kzg4844.Blob
	for i := 0; i < 4096; i++ {
		cell := new(big.Int).SetBytes(blob[i*32 : (i+1)*32])
		witness.Blob[i] = emulated.ValueOf[FE](cell)
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
