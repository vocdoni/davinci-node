package statetransitiontest

import (
	"math/big"
	"os"
	"testing"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/test"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	spechash "github.com/vocdoni/davinci-node/spec/hash"
	"github.com/vocdoni/davinci-node/spec/params"
	statetest "github.com/vocdoni/davinci-node/state/testutil"
	"github.com/vocdoni/davinci-node/types"
)

type CircuitProcessProofKeys struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitProcessProofKeys) Define(api frontend.API) error {
	circuit.VerifyProcessProofKeys(api)
	return nil
}

type CircuitSupportedCensusOrigin struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitSupportedCensusOrigin) Define(api frontend.API) error {
	circuit.VerifyIsValidCensusOrigin(api)
	return nil
}

func TestProcessProofKeysPinned(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseString {
		t.Skip("skipping circuit tests...")
	}

	assignment := NewTransitionWithOverwrittenVotes(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	assignment.ProcessProofs.CensusOrigin = assignment.ProcessProofs.EncryptionKey

	assert := test.NewAssert(t)
	assert.CheckCircuit(
		&CircuitProcessProofKeys{},
		test.WithInvalidAssignment(assignment),
		test.WithCurves(params.StateTransitionCurve),
		test.WithBackends(backend.GROTH16),
	)
}

func TestSupportedCensusOrigin(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseString {
		t.Skip("skipping circuit tests...")
	}

	assignment := NewTransitionWithOverwrittenVotes(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	encKeyHash, err := spechash.PoseidonHash(
		assignment.Process.EncryptionKey.PubKey[0].(*big.Int),
		assignment.Process.EncryptionKey.PubKey[1].(*big.Int),
	)
	if err != nil {
		t.Fatal(err)
	}
	assignment.Process.CensusOrigin = encKeyHash

	assert := test.NewAssert(t)
	assert.CheckCircuit(
		&CircuitSupportedCensusOrigin{},
		test.WithInvalidAssignment(assignment),
		test.WithCurves(params.StateTransitionCurve),
		test.WithBackends(backend.GROTH16),
	)
}

// TestDummySlot verifies that a "dummy" slot (index >= VotersCount)
// cannot contain any state transition (Insert/Update).
func TestDummySlot(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseString {
		t.Skip("skipping circuit tests...")
	}

	s := statetest.NewRandomState(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	publicKey := statetest.EncryptionKeyAsECCPoint(s)

	// Create a transition with 2 votes (index 0 and 1)
	// We will try to "hide" the second vote (index 1) by claiming VotersCount is 1.
	assignment := NewTransitionWithVotes(t, s,
		statetest.NewVoteForTest(publicKey, 1, 10), // valid vote 1
		statetest.NewVoteForTest(publicKey, 2, 20), // valid vote 2
	)

	// Hack the assignment: reduce VotersCount from 2 to 1.
	// This makes the vote at index 1 a "dummy" vote according to the circuit logic.
	// However, the MerkleProof for index 1 is still a valid Insert/Update.
	assignment.VotersCount = 1

	// Assert that the circuit rejects this assignment.
	// The fix in VerifyMerkleTransitions and VerifyBallots should assert that
	// for dummy slots (isRealVote=0), the operations must be NOOP.
	assert := test.NewAssert(t)
	// We expect the prover to FAIL because the constraints are not satisfied.
	assert.CheckCircuit(
		CircuitPlaceholderWithProof(&assignment.AggregatorProof, &assignment.AggregatorVK),
		test.WithInvalidAssignment(assignment),
		test.WithCurves(params.StateTransitionCurve),
		test.WithBackends(backend.GROTH16),
	)
}
