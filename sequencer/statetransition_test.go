package sequencer

import (
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/ethereum/go-ethereum/accounts/abi"
	qt "github.com/frankban/quicktest"
	stc "github.com/vocdoni/davinci-node/circuits/statetransition"
	statetransitiontest "github.com/vocdoni/davinci-node/circuits/test/statetransition"
	"github.com/vocdoni/davinci-node/internal/testutil"
	spechash "github.com/vocdoni/davinci-node/spec/hash"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	"github.com/vocdoni/davinci-node/state"
	statetest "github.com/vocdoni/davinci-node/state/testutil"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
	leanimt "github.com/vocdoni/lean-imt-go"
	leancensus "github.com/vocdoni/lean-imt-go/census"
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

// makeTestProcessWithCensus creates a process in storage with the given MerkleTree census root.
func makeTestProcessWithCensus(t *testing.T, stg *storage.Storage, processID types.ProcessID, censusRoot types.HexBytes) {
	t.Helper()
	encryptionKey := testutil.RandomEncryptionPubKey()
	censusOrigin := types.CensusOriginMerkleTreeOffchainStaticV1
	stateRoot, err := spechash.StateRoot(
		processID.MathBigInt(),
		censusOrigin.BigInt().MathBigInt(),
		encryptionKey.X.MathBigInt(),
		encryptionKey.Y.MathBigInt(),
		testutil.BallotModePacked(),
	)
	if err != nil {
		t.Fatalf("spechash.StateRoot: %v", err)
	}
	proc := &types.Process{
		ID:            &processID,
		Status:        types.ProcessStatusReady,
		StartTime:     time.Now(),
		Duration:      time.Hour,
		MetadataURI:   testMetadataURI,
		BallotMode:    testutil.BallotMode(),
		EncryptionKey: &encryptionKey,
		StateRoot:     types.BigIntConverter(stateRoot),
		Census: &types.Census{
			CensusOrigin: censusOrigin,
			CensusRoot:   censusRoot,
		},
	}
	if err := stg.NewProcess(proc); err != nil {
		t.Fatalf("NewProcess: %v", err)
	}
}

// TestProcessCensusProofsNilRootReturnsError verifies that processCensusProofs
// returns an error when the census tree exists in storage but has no root
// (empty tree, no entries).
func TestProcessCensusProofsNilRootReturnsError(t *testing.T) {
	c := qt.New(t)
	stg := newTestSequencerStorage(t)
	defer stg.Close()

	processID := testutil.RandomProcessID()
	censusRoot := types.HexBytes(testutil.RandomCensusRoot().Bytes())

	// Register an empty census (no entries) so LoadCensus succeeds but
	// Tree().Root() returns (nil, false).
	_, err := stg.CensusDB().NewByRoot(censusRoot)
	c.Assert(err, qt.IsNil)
	makeTestProcessWithCensus(t, stg, processID, censusRoot)

	seq := &Sequencer{stg: stg, processIDs: NewProcessIDMap()}

	_, _, err = seq.processCensusProofs(processID, nil, nil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "census tree has no root")
}

// TestProcessCensusProofsMissingAddressReturnsError verifies that processCensusProofs
// returns an error when a vote's address is absent from the census tree. Census
// filtering must happen before aggregation (to preserve the aggregator proof's
// BatchHash public input); a missing address at state-transition time is fatal.
func TestProcessCensusProofsMissingAddressReturnsError(t *testing.T) {
	c := qt.New(t)
	stg := newTestSequencerStorage(t)
	defer stg.Close()

	processID := testutil.RandomProcessID()

	addr0 := testutil.DeterministicAddress(0)
	addr1 := testutil.DeterministicAddress(1)
	sourceTree, err := leancensus.NewCensusIMT(nil, leanimt.PoseidonHasher)
	c.Assert(err, qt.IsNil)
	c.Assert(sourceTree.Add(addr0, big.NewInt(1)), qt.IsNil)
	c.Assert(sourceTree.Add(addr1, big.NewInt(1)), qt.IsNil)
	root, ok := sourceTree.Root()
	c.Assert(ok, qt.IsTrue)

	censusRoot := types.HexBytes(root.Bytes())
	_, err = stg.CensusDB().Import(censusRoot, sourceTree.Dump())
	c.Assert(err, qt.IsNil)
	makeTestProcessWithCensus(t, stg, processID, censusRoot)

	// addr0 and addr1 are in the census; addr2 (index 999) is not.
	addr2 := testutil.DeterministicAddress(999)
	votes := []*state.Vote{
		{Address: addr0.Big(), Weight: big.NewInt(1)},
		{Address: addr1.Big(), Weight: big.NewInt(1)},
		{Address: addr2.Big(), Weight: big.NewInt(1)},
	}

	seq := &Sequencer{stg: stg, processIDs: NewProcessIDMap()}
	_, _, err = seq.processCensusProofs(processID, votes, nil)
	c.Assert(err, qt.Not(qt.IsNil))
}

// TestProcessCensusProofsAllValidReturnsAllVotes verifies that when every vote
// address is present in the census tree, all votes are returned unchanged.
func TestProcessCensusProofsAllValidReturnsAllVotes(t *testing.T) {
	c := qt.New(t)
	stg := newTestSequencerStorage(t)
	defer stg.Close()

	processID := testutil.RandomProcessID()

	addr0 := testutil.DeterministicAddress(0)
	addr1 := testutil.DeterministicAddress(1)
	sourceTree, err := leancensus.NewCensusIMT(nil, leanimt.PoseidonHasher)
	c.Assert(err, qt.IsNil)
	c.Assert(sourceTree.Add(addr0, big.NewInt(1)), qt.IsNil)
	c.Assert(sourceTree.Add(addr1, big.NewInt(1)), qt.IsNil)
	root, ok := sourceTree.Root()
	c.Assert(ok, qt.IsTrue)

	censusRoot := types.HexBytes(root.Bytes())
	_, err = stg.CensusDB().Import(censusRoot, sourceTree.Dump())
	c.Assert(err, qt.IsNil)
	makeTestProcessWithCensus(t, stg, processID, censusRoot)

	votes := []*state.Vote{
		{Address: addr0.Big(), Weight: big.NewInt(1)},
		{Address: addr1.Big(), Weight: big.NewInt(1)},
	}

	seq := &Sequencer{stg: stg, processIDs: NewProcessIDMap()}
	_, _, err = seq.processCensusProofs(processID, votes, nil)
	c.Assert(err, qt.IsNil)
}
