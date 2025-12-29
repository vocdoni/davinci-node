package blobs

import (
	"math/big"
	"os"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/hash/poseidon"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

// testEvaluationPointCircuit is a simple circuit that computes the evaluation point
// using the in-circuit implementation
type testEvaluationPointCircuit struct {
	ProcessID       frontend.Variable    `gnark:",public"`
	RootHashBefore  frontend.Variable    `gnark:",public"`
	CommitmentLimbs [3]frontend.Variable `gnark:",public"`
	ExpectedZ       frontend.Variable    `gnark:",public"`
}

func (circuit *testEvaluationPointCircuit) Define(api frontend.API) error {
	// Compute z in-circuit
	z, err := ComputeEvaluationPointInCircuit(api, circuit.ProcessID, circuit.RootHashBefore, circuit.CommitmentLimbs)
	if err != nil {
		return err
	}

	// Assert it matches the expected value
	api.AssertIsEqual(z, circuit.ExpectedZ)
	return nil
}

// TestComputeEvaluationPointConsistency verifies that both the Go off-circuit
// and Gnark in-circuit implementations produce the same evaluation point z.
func TestComputeEvaluationPointConsistency(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseStr {
		t.Skip("skipping circuit tests...")
	}
	c := qt.New(t)

	// Create a test blob
	blob := &types.Blob{}
	for i := range 50 {
		val := big.NewInt(int64(i + 1))
		valHash, err := poseidon.MultiPoseidon(val)
		c.Assert(err, qt.IsNil)
		valHash.FillBytes(blob[i*32 : (i+1)*32])
	}

	// Compute commitment
	commitment, err := blob.ComputeCommitment()
	c.Assert(err, qt.IsNil)

	// Test with random inputs
	processID := new(big.Int).SetBytes(util.RandomBytes(31))
	rootHashBefore := new(big.Int).SetBytes(util.RandomBytes(31))

	// Compute z using Go off-circuit implementation
	zOffCircuit, err := ComputeEvaluationPoint(processID, rootHashBefore, commitment)
	c.Assert(err, qt.IsNil)

	// Split commitment into limbs for circuit
	commitmentLimbs := CommitmentToLimbs(commitment)

	// Create witness for circuit
	witness := testEvaluationPointCircuit{
		ProcessID:       processID,
		RootHashBefore:  rootHashBefore,
		CommitmentLimbs: [3]frontend.Variable{commitmentLimbs[0], commitmentLimbs[1], commitmentLimbs[2]},
		ExpectedZ:       zOffCircuit,
	}

	// Test that the circuit accepts this witness (proving both implementations match)
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&testEvaluationPointCircuit{}, &witness,
		test.WithCurves(ecc.BN254), test.WithBackends(backend.GROTH16))
}

// TestComputeEvaluationPointMultipleCases tests consistency across multiple test cases
func TestComputeEvaluationPointMultipleCases(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseStr {
		t.Skip("skipping circuit tests...")
	}
	c := qt.New(t)

	testCases := []struct {
		name     string
		blobSize int
	}{
		{"Small blob (10 elements)", 10},
		{"Medium blob (100 elements)", 100},
		{"Large blob (500 elements)", 500},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create test blob
			blob := &types.Blob{}
			for i := range tc.blobSize {
				val := big.NewInt(int64(i + 1))
				valHash, err := poseidon.MultiPoseidon(val)
				c.Assert(err, qt.IsNil)
				valHash.FillBytes(blob[i*32 : (i+1)*32])
			}

			// Compute commitment
			commitment, err := blob.ComputeCommitment()
			c.Assert(err, qt.IsNil)

			// Random inputs
			processID := new(big.Int).SetBytes(util.RandomBytes(31))
			rootHashBefore := new(big.Int).SetBytes(util.RandomBytes(31))

			// Compute z using Go implementation
			zOffCircuit, err := ComputeEvaluationPoint(processID, rootHashBefore, commitment)
			c.Assert(err, qt.IsNil)

			// Split commitment into limbs
			commitmentLimbs := CommitmentToLimbs(commitment)

			// Create witness
			witness := testEvaluationPointCircuit{
				ProcessID:       processID,
				RootHashBefore:  rootHashBefore,
				CommitmentLimbs: [3]frontend.Variable{commitmentLimbs[0], commitmentLimbs[1], commitmentLimbs[2]},
				ExpectedZ:       zOffCircuit,
			}

			// Verify circuit accepts the witness
			assert := test.NewAssert(t)
			assert.SolvingSucceeded(&testEvaluationPointCircuit{}, &witness,
				test.WithCurves(circuits.StateTransitionCurve), test.WithBackends(backend.GROTH16))
		})
	}
}

// TestCommitmentToLimbs verifies the commitment limb splitting
func TestCommitmentToLimbs(t *testing.T) {
	c := qt.New(t)

	// Create a test commitment (48 bytes)
	commitment := types.KZGCommitment{}
	for i := range 48 {
		commitment[i] = byte(i)
	}

	// Split into limbs
	limbs := CommitmentToLimbs(commitment)

	// Verify we have 3 limbs
	c.Assert(len(limbs), qt.Equals, 3)

	// Verify each limb is 16 bytes
	limb1Bytes := limbs[0].Bytes()
	limb2Bytes := limbs[1].Bytes()
	limb3Bytes := limbs[2].Bytes()

	// Reconstruct and verify
	reconstructed := make([]byte, 48)
	copy(reconstructed[16-len(limb1Bytes):16], limb1Bytes)
	copy(reconstructed[32-len(limb2Bytes):32], limb2Bytes)
	copy(reconstructed[48-len(limb3Bytes):48], limb3Bytes)

	c.Assert(reconstructed, qt.DeepEquals, commitment[:])
}
