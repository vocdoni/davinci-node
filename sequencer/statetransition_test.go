package sequencer

import (
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/ethereum/go-ethereum/accounts/abi"
	qt "github.com/frankban/quicktest"
	stc "github.com/vocdoni/davinci-node/circuits/statetransition"
	statetransitiontest "github.com/vocdoni/davinci-node/circuits/test/statetransition"
	"github.com/vocdoni/davinci-node/internal/testutil"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	statetest "github.com/vocdoni/davinci-node/state/testutil"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

func testVariableAsBigInt(t *testing.T, v any) *big.Int {
	t.Helper()
	switch x := v.(type) {
	case *big.Int:
		return x
	case int:
		return big.NewInt(int64(x))
	case uint64:
		return new(big.Int).SetUint64(x)
	default:
		t.Fatalf("unexpected variable type %T", v)
		return nil
	}
}

func TestProcessPendingTransitionsDoesNotReserveAggregatorBatchWhenTransitionTxPending(t *testing.T) {
	c := qt.New(t)
	stg := newTestSequencerStorage(t)
	defer stg.Close()

	processID := testutil.RandomProcessID()
	ensureSequencerTestProcess(t, stg, processID)

	batch := &storage.AggregatorBallotBatch{
		ProcessID: processID,
		Ballots: []*storage.AggregatorBallot{
			{VoteID: testutil.RandomVoteID()},
		},
	}
	c.Assert(stg.PushAggregatorBatch(batch), qt.IsNil)
	c.Assert(stg.SetPendingTx(storage.StateTransitionTx, processID), qt.IsNil)

	seq := &Sequencer{
		stg: stg,
		contractsResolver: &testContractsResolver{
			contractsByProcess: map[types.ProcessID]*web3.Contracts{
				processID: {},
			},
		},
		processIDs: NewProcessIDMap(),
	}
	c.Assert(seq.processIDs.Add(processID), qt.IsTrue)

	seq.processPendingTransitions()

	got, batchID, err := stg.NextAggregatorBatch(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(got.Ballots, qt.HasLen, 1)
	c.Assert(got.Ballots[0].VoteID, qt.Equals, batch.Ballots[0].VoteID)
	c.Assert(stg.MarkAggregatorBatchDone(batchID), qt.IsNil)
}

func TestProcessStateTransitionBatchRollsBackRootOnAssignmentError(t *testing.T) {
	c := qt.New(t)

	processState := statetest.NewRandomState(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	rootBefore, err := processState.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	publicKey := statetest.EncryptionKeyAsECCPoint(processState)
	votes := statetest.NewVotesForTest(publicKey, 2, 10)

	kSeed, err := specutil.RandomK()
	c.Assert(err, qt.IsNil)

	lastK := new(types.BigInt).SetBigInt(kSeed).MathBigInt()
	for _, vote := range votes {
		vote.ReencryptedBallot, lastK, err = vote.Ballot.Reencrypt(publicKey, lastK)
		c.Assert(err, qt.IsNil)
	}

	processID, err := types.BigIntToProcessID(processState.ProcessID())
	c.Assert(err, qt.IsNil)

	censusRoot, censusProofs, err := statetransitiontest.CensusProofsForCircuitTest(
		t,
		votes,
		types.CensusOriginMerkleTreeOffchainStaticV1,
		processID,
	)
	c.Assert(err, qt.IsNil)

	var innerProof groth16.Proof

	_, _, err = new(Sequencer).processStateTransitionBatch(
		processState,
		new(types.BigInt).SetBigInt(censusRoot),
		censusProofs,
		votes,
		new(types.BigInt).SetBigInt(kSeed),
		innerProof,
	)
	c.Assert(err, qt.Not(qt.IsNil))

	rootAfter, err := processState.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(rootAfter.Cmp(rootBefore), qt.Equals, 0)

	containsVote := processState.ContainsVoteID(votes[0].VoteID)
	c.Assert(containsVote, qt.IsFalse)
}

func TestStateBatchToAssignmentRollsBackOnLateError(t *testing.T) {
	c := qt.New(t)

	processState := statetest.NewRandomState(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	rootBefore, err := processState.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	publicKey := statetest.EncryptionKeyAsECCPoint(processState)
	votes := statetest.NewVotesForTest(publicKey, 2, 10)

	kSeed, err := specutil.RandomK()
	c.Assert(err, qt.IsNil)

	lastK := new(types.BigInt).SetBigInt(kSeed).MathBigInt()
	for _, vote := range votes {
		vote.ReencryptedBallot, lastK, err = vote.Ballot.Reencrypt(publicKey, lastK)
		c.Assert(err, qt.IsNil)
	}

	processID, err := types.BigIntToProcessID(processState.ProcessID())
	c.Assert(err, qt.IsNil)

	censusRoot, censusProofs, err := statetransitiontest.CensusProofsForCircuitTest(
		t,
		votes,
		types.CensusOriginMerkleTreeOffchainStaticV1,
		processID,
	)
	c.Assert(err, qt.IsNil)

	var innerProof groth16.Proof

	_, _, err = new(Sequencer).stateBatchToAssignment(
		processState,
		votes,
		new(types.BigInt).SetBigInt(censusRoot),
		censusProofs,
		new(types.BigInt).SetBigInt(kSeed),
		innerProof,
	)
	c.Assert(err, qt.Not(qt.IsNil))

	rootAfter, err := processState.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(rootAfter.Cmp(rootBefore), qt.Equals, 0)

	containsVote := processState.ContainsVoteID(votes[0].VoteID)
	c.Assert(containsVote, qt.IsFalse)
}

func TestBuildStateTransitionBatchProofInputsUsesBatchStateCounters(t *testing.T) {
	c := qt.New(t)

	processState := statetest.NewRandomState(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	rootBefore, err := processState.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	publicKey := statetest.EncryptionKeyAsECCPoint(processState)
	votes := statetest.NewVotesForTest(publicKey, 3, 10)
	batch, err := processState.PrepareVotesBatch(votes)
	c.Assert(err, qt.IsNil)
	defer batch.Discard()

	inputs := buildStateTransitionBatchProofInputs(
		batch,
		new(types.BigInt).SetBigInt(big.NewInt(123)),
	)

	c.Assert(inputs.RootHashBefore.Cmp(rootBefore), qt.Equals, 0)
	c.Assert(inputs.VotersCount, qt.Equals, len(votes))
	c.Assert(inputs.OverwrittenVotesCount, qt.Equals, 0)
	c.Assert(inputs.CensusRoot.Cmp(big.NewInt(123)), qt.Equals, 0)
	c.Assert(batch.VotersCount(), qt.Equals, len(votes))
}

func TestBuildStateTransitionBatchProofInputsMatchesCircuitPublicInputs(t *testing.T) {
	c := qt.New(t)

	processState := statetest.NewRandomState(t, types.CensusOriginMerkleTreeOffchainStaticV1)

	publicKey := statetest.EncryptionKeyAsECCPoint(processState)
	votes := statetest.NewVotesForTest(publicKey, 2, 10)
	batch, err := processState.PrepareVotesBatch(votes)
	c.Assert(err, qt.IsNil)
	defer batch.Discard()

	processID, err := types.BigIntToProcessID(processState.ProcessID())
	c.Assert(err, qt.IsNil)

	censusRoot, censusProofs, err := statetransitiontest.CensusProofsForCircuitTest(
		t,
		votes,
		types.CensusOriginMerkleTreeOffchainStaticV1,
		processID,
	)
	c.Assert(err, qt.IsNil)

	kSeed, err := specutil.RandomK()
	c.Assert(err, qt.IsNil)

	assignment, publicInputs, err := stc.GenerateAssignment(
		batch,
		new(types.BigInt).SetBigInt(censusRoot),
		censusProofs,
		new(types.BigInt).SetBigInt(kSeed),
	)
	c.Assert(err, qt.IsNil)

	inputs := buildStateTransitionBatchProofInputs(
		batch,
		new(types.BigInt).SetBigInt(publicInputs.CensusRoot),
	)

	c.Assert(inputs.RootHashBefore.Cmp(publicInputs.RootHashBefore), qt.Equals, 0)
	c.Assert(inputs.RootHashAfter.Cmp(publicInputs.RootHashAfter), qt.Equals, 0)
	c.Assert(inputs.VotersCount, qt.Equals, int(publicInputs.VotersCount.Int64()))
	c.Assert(inputs.OverwrittenVotesCount, qt.Equals, int(publicInputs.OverwrittenVotesCount.Int64()))
	c.Assert(inputs.CensusRoot.Cmp(publicInputs.CensusRoot), qt.Equals, 0)
	c.Assert(inputs.BlobCommitmentLimbs[0].Cmp(publicInputs.BlobCommitmentLimbs[0]), qt.Equals, 0)
	c.Assert(inputs.BlobCommitmentLimbs[1].Cmp(publicInputs.BlobCommitmentLimbs[1]), qt.Equals, 0)
	c.Assert(inputs.BlobCommitmentLimbs[2].Cmp(publicInputs.BlobCommitmentLimbs[2]), qt.Equals, 0)
	c.Assert(testVariableAsBigInt(t, assignment.BlobCommitmentLimbs[0]).Cmp(publicInputs.BlobCommitmentLimbs[0]), qt.Equals, 0)
	c.Assert(testVariableAsBigInt(t, assignment.BlobCommitmentLimbs[1]).Cmp(publicInputs.BlobCommitmentLimbs[1]), qt.Equals, 0)
	c.Assert(testVariableAsBigInt(t, assignment.BlobCommitmentLimbs[2]).Cmp(publicInputs.BlobCommitmentLimbs[2]), qt.Equals, 0)
}

func TestStateTransitionProofVerifiesWithABIDecodedPublicInputs(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == "false" {
		t.Skip("skipping circuit tests...")
	}

	c := qt.New(t)

	testResults, placeholder, assignment := statetransitiontest.StateTransitionInputsForTest(
		t,
		testutil.FixedProcessID(),
		types.CensusOriginMerkleTreeOffchainStaticV1,
		3,
	)

	statetransitionRuntime, err := stc.Artifacts.LoadOrSetupForCircuit(t.Context(), placeholder)
	c.Assert(err, qt.IsNil)

	proof, err := statetransitionRuntime.Prove(assignment)
	c.Assert(err, qt.IsNil)

	publicInputs := testResults.PublicInputs
	inputs := storage.StateTransitionBatchProofInputs{
		RootHashBefore:        publicInputs.RootHashBefore,
		RootHashAfter:         publicInputs.RootHashAfter,
		VotersCount:           int(publicInputs.VotersCount.Int64()),
		OverwrittenVotesCount: int(publicInputs.OverwrittenVotesCount.Int64()),
		CensusRoot:            publicInputs.CensusRoot,
		BlobCommitmentLimbs:   publicInputs.BlobCommitmentLimbs,
	}
	encodedInputs, err := inputs.ABIEncode()
	c.Assert(err, qt.IsNil)

	decodedInputs, err := decodeStateTransitionBatchProofInputs(encodedInputs)
	c.Assert(err, qt.IsNil)

	publicOnlyAssignment := publicStateTransitionCircuitFromInputs(decodedInputs)
	c.Assert(statetransitionRuntime.Verify(proof, publicOnlyAssignment), qt.IsNil)
}

func decodeStateTransitionBatchProofInputs(encodedInputs []byte) (decoded storage.StateTransitionBatchProofInputs, err error) {
	inputType, err := abi.NewType("uint256[8]", "", nil)
	if err != nil {
		return decoded, err
	}
	arguments := abi.Arguments{{Type: inputType}}
	unpacked, err := arguments.Unpack(encodedInputs)
	if err != nil {
		return decoded, err
	}
	if len(unpacked) != 1 {
		return decoded, fmt.Errorf("unexpected unpacked input count: %d", len(unpacked))
	}
	values, ok := unpacked[0].([8]*big.Int)
	if !ok {
		return decoded, fmt.Errorf("unexpected ABI input type: %T", unpacked[0])
	}
	decoded.RootHashBefore = values[0]
	decoded.RootHashAfter = values[1]
	decoded.VotersCount = int(values[2].Uint64())
	decoded.OverwrittenVotesCount = int(values[3].Uint64())
	decoded.CensusRoot = values[4]
	decoded.BlobCommitmentLimbs[0] = values[5]
	decoded.BlobCommitmentLimbs[1] = values[6]
	decoded.BlobCommitmentLimbs[2] = values[7]
	return decoded, nil
}

func publicStateTransitionCircuitFromInputs(inputs storage.StateTransitionBatchProofInputs) *stc.StateTransitionCircuit {
	circuit := &stc.StateTransitionCircuit{
		RootHashBefore:        inputs.RootHashBefore,
		RootHashAfter:         inputs.RootHashAfter,
		VotersCount:           inputs.VotersCount,
		OverwrittenVotesCount: inputs.OverwrittenVotesCount,
		CensusRoot:            inputs.CensusRoot,
	}
	circuit.BlobCommitmentLimbs[0] = inputs.BlobCommitmentLimbs[0]
	circuit.BlobCommitmentLimbs[1] = inputs.BlobCommitmentLimbs[1]
	circuit.BlobCommitmentLimbs[2] = inputs.BlobCommitmentLimbs[2]
	return circuit
}
