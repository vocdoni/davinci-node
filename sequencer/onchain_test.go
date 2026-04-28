package sequencer

import (
	"errors"
	"math/big"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	spechash "github.com/vocdoni/davinci-node/spec/hash"
	"github.com/vocdoni/davinci-node/state"
	statetest "github.com/vocdoni/davinci-node/state/testutil"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

func newTestSequencerStorage(t *testing.T) *storage.Storage {
	t.Helper()
	dbDir := t.TempDir()
	testdb, err := metadb.New(db.TypePebble, dbDir)
	if err != nil {
		t.Fatalf("metadb.New: %v", err)
	}
	return storage.New(testdb)
}

func ensureSequencerTestProcess(t *testing.T, stg *storage.Storage, pid types.ProcessID) {
	t.Helper()

	censusRoot := make([]byte, types.CensusRootLength)
	encryptionKey := testutil.RandomEncryptionPubKey()
	censusOrigin := types.CensusOriginMerkleTreeOffchainStaticV1
	stateRoot, err := spechash.StateRoot(
		pid.MathBigInt(),
		censusOrigin.BigInt().MathBigInt(),
		encryptionKey.X.MathBigInt(),
		encryptionKey.Y.MathBigInt(),
		testutil.BallotModePacked(),
	)
	if err != nil {
		t.Fatalf("spechash.StateRoot(%x): %v", pid.Bytes(), err)
	}

	proc := &types.Process{
		ID:            &pid,
		Status:        types.ProcessStatusReady,
		StartTime:     time.Now(),
		Duration:      time.Hour,
		MetadataURI:   "http://example.com/metadata",
		BallotMode:    testutil.BallotMode(),
		EncryptionKey: &encryptionKey,
		StateRoot:     types.BigIntConverter(stateRoot),
		Census: &types.Census{
			CensusOrigin: censusOrigin,
			CensusRoot:   types.HexBytes(censusRoot),
		},
	}
	if err := stg.NewProcess(proc); err != nil {
		t.Fatalf("NewProcess(%x): %v", pid.Bytes(), err)
	}
}

func testSequencerAggBallot(voteID types.VoteID) *storage.AggregatorBallot {
	return &storage.AggregatorBallot{VoteID: voteID}
}

func TestPushStateTransitionCallbackMarksBatchDoneAfterSuccess(t *testing.T) {
	c := qt.New(t)
	stg := newTestSequencerStorage(t)
	defer stg.Close()

	processID := testutil.RandomProcessID()
	encryptionKey, _, err := elgamal.GenerateKey(state.Curve)
	c.Assert(err, qt.IsNil)
	ballotMode := testutil.BallotMode()
	packedBallotMode, err := ballotMode.Pack()
	c.Assert(err, qt.IsNil)
	rootBefore, err := spechash.StateRoot(
		processID.MathBigInt(),
		types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(),
		encryptionKey.BigInts()[0],
		encryptionKey.BigInts()[1],
		packedBallotMode,
	)
	c.Assert(err, qt.IsNil)
	voteIDs := []types.VoteID{
		testutil.RandomVoteID(),
		testutil.RandomVoteID(),
	}
	process := &types.Process{
		ID:          &processID,
		Status:      types.ProcessStatusReady,
		StartTime:   time.Now(),
		Duration:    time.Hour,
		MetadataURI: "http://example.com/metadata",
		BallotMode:  ballotMode,
		EncryptionKey: func() *types.EncryptionKey {
			key := types.EncryptionKeyFromPoint(encryptionKey)
			return &key
		}(),
		StateRoot: types.BigIntConverter(rootBefore),
		Census: &types.Census{
			CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
			CensusRoot:   types.HexBytes(make([]byte, types.CensusRootLength)),
		},
	}
	c.Assert(stg.NewProcess(process), qt.IsNil)

	transitionState, err := state.New(memdb.New(), processID)
	c.Assert(err, qt.IsNil)
	c.Assert(transitionState.Initialize(
		types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(),
		packedBallotMode,
		types.EncryptionKeyFromPoint(encryptionKey),
	), qt.IsNil)
	votes := statetest.NewVotesForTest(encryptionKey, 2, 1)
	batch, err := transitionState.PrepareVotesBatch(votes)
	c.Assert(err, qt.IsNil)
	rootAfter, err := batch.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(batch.Commit(), qt.IsNil)
	blobSidecar := batch.BlobEvalData().TxSidecar()

	c.Assert(stg.PushStateTransitionArtifact(&state.TransitionArtifact{
		ProcessID:       processID,
		RootHashBefore:  rootBefore,
		RootHashAfter:   rootAfter,
		BlobVersionHash: blobSidecar.BlobHashes()[0],
		BlobSidecar:     blobSidecar,
	}), qt.IsNil)

	stb := &storage.StateTransitionBatch{
		ProcessID: processID,
		Ballots: []*storage.AggregatorBallot{
			testSequencerAggBallot(voteIDs[0]),
			testSequencerAggBallot(voteIDs[1]),
		},
		Inputs: storage.StateTransitionBatchProofInputs{
			RootHashBefore: rootBefore,
			RootHashAfter:  rootAfter,
			CensusRoot:     big.NewInt(3),
		},
	}
	c.Assert(stg.PushStateTransitionBatch(stb), qt.IsNil)

	_, batchID, err := stg.NextStateTransitionBatch(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(stg.SetPendingTx(storage.StateTransitionTx, processID), qt.IsNil)
	c.Assert(stg.HasPendingTx(storage.StateTransitionTx, processID), qt.IsTrue)

	seq := &Sequencer{
		stg:        stg,
		processIDs: NewProcessIDMap(),
	}

	seq.pushStateTransitionCallback(processID, batchID, rootAfter)(nil)

	c.Assert(stg.HasPendingTx(storage.StateTransitionTx, processID), qt.IsFalse)

	_, _, err = stg.NextStateTransitionBatch(processID)
	c.Assert(errors.Is(err, storage.ErrNoMoreElements), qt.IsTrue)

	promotedState, err := state.LoadSnapshotOnRoot(stg.StateDB(), processID, rootAfter)
	c.Assert(err, qt.IsNil)
	gotRoot, err := promotedState.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(gotRoot.Cmp(rootAfter), qt.Equals, 0)

	for _, voteID := range voteIDs {
		status, err := stg.VoteIDStatus(processID, voteID)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, storage.VoteIDStatusDone)
	}
}

func TestPushStateTransitionCallbackFailsWhenArtifactMissing(t *testing.T) {
	c := qt.New(t)
	stg := newTestSequencerStorage(t)
	defer stg.Close()

	processID := testutil.RandomProcessID()
	voteID := testutil.RandomVoteID()
	ensureSequencerTestProcess(t, stg, processID)

	stb := &storage.StateTransitionBatch{
		ProcessID: processID,
		Ballots: []*storage.AggregatorBallot{
			testSequencerAggBallot(voteID),
		},
		Inputs: storage.StateTransitionBatchProofInputs{
			RootHashBefore: big.NewInt(1),
			RootHashAfter:  big.NewInt(2),
			CensusRoot:     big.NewInt(3),
		},
	}
	c.Assert(stg.PushStateTransitionBatch(stb), qt.IsNil)

	_, batchID, err := stg.NextStateTransitionBatch(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(stg.SetPendingTx(storage.StateTransitionTx, processID), qt.IsNil)
	c.Assert(stg.HasPendingTx(storage.StateTransitionTx, processID), qt.IsTrue)

	seq := &Sequencer{
		stg:        stg,
		processIDs: NewProcessIDMap(),
	}

	seq.pushStateTransitionCallback(processID, batchID, big.NewInt(2))(nil)

	c.Assert(stg.HasPendingTx(storage.StateTransitionTx, processID), qt.IsFalse)

	_, _, err = stg.NextStateTransitionBatch(processID)
	c.Assert(errors.Is(err, storage.ErrNoMoreElements), qt.IsTrue)
	c.Assert(seq.processIDs.Exists(processID), qt.IsFalse)

	_, err = state.LoadSnapshotOnRoot(stg.StateDB(), processID, big.NewInt(2))
	c.Assert(err, qt.Not(qt.IsNil))

	status, err := stg.VoteIDStatus(processID, voteID)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, storage.VoteIDStatusError)
}

func TestPushStateTransitionCallbackMarksBatchFailedOnError(t *testing.T) {
	c := qt.New(t)
	stg := newTestSequencerStorage(t)
	defer stg.Close()

	processID := testutil.RandomProcessID()
	voteID := testutil.RandomVoteID()
	ensureSequencerTestProcess(t, stg, processID)

	stb := &storage.StateTransitionBatch{
		ProcessID: processID,
		Ballots: []*storage.AggregatorBallot{
			testSequencerAggBallot(voteID),
		},
		Inputs: storage.StateTransitionBatchProofInputs{
			RootHashBefore: big.NewInt(1),
			RootHashAfter:  big.NewInt(2),
			CensusRoot:     big.NewInt(3),
		},
	}
	c.Assert(stg.PushStateTransitionBatch(stb), qt.IsNil)

	_, batchID, err := stg.NextStateTransitionBatch(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(stg.SetPendingTx(storage.StateTransitionTx, processID), qt.IsNil)
	c.Assert(stg.HasPendingTx(storage.StateTransitionTx, processID), qt.IsTrue)

	seq := &Sequencer{
		stg:        stg,
		processIDs: NewProcessIDMap(),
	}

	seq.pushStateTransitionCallback(processID, batchID, big.NewInt(2))(errors.New("mining failed"))

	c.Assert(stg.HasPendingTx(storage.StateTransitionTx, processID), qt.IsFalse)

	_, _, err = stg.NextStateTransitionBatch(processID)
	c.Assert(errors.Is(err, storage.ErrNoMoreElements), qt.IsTrue)

	status, err := stg.VoteIDStatus(processID, voteID)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, storage.VoteIDStatusError)
}
