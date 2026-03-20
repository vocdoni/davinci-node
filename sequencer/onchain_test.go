package sequencer

import (
	"errors"
	"math/big"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	spechash "github.com/vocdoni/davinci-node/spec/hash"
	"github.com/vocdoni/davinci-node/spec/params"
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
		pid.ToFF(params.StateTransitionCurve.ScalarField()).MathBigInt(),
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
	voteIDs := []types.VoteID{
		testutil.RandomVoteID(),
		testutil.RandomVoteID(),
	}
	ensureSequencerTestProcess(t, stg, processID)

	stb := &storage.StateTransitionBatch{
		ProcessID: processID,
		Ballots: []*storage.AggregatorBallot{
			testSequencerAggBallot(voteIDs[0]),
			testSequencerAggBallot(voteIDs[1]),
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

	seq.pushStateTransitionCallback(processID, batchID)(nil)

	c.Assert(stg.HasPendingTx(storage.StateTransitionTx, processID), qt.IsFalse)

	_, _, err = stg.NextStateTransitionBatch(processID)
	c.Assert(errors.Is(err, storage.ErrNoMoreElements), qt.IsTrue)

	for _, voteID := range voteIDs {
		status, err := stg.VoteIDStatus(processID, voteID)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, storage.VoteIDStatusDone)
	}
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

	seq.pushStateTransitionCallback(processID, batchID)(errors.New("mining failed"))

	c.Assert(stg.HasPendingTx(storage.StateTransitionTx, processID), qt.IsFalse)

	_, _, err = stg.NextStateTransitionBatch(processID)
	c.Assert(errors.Is(err, storage.ErrNoMoreElements), qt.IsTrue)

	status, err := stg.VoteIDStatus(processID, voteID)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, storage.VoteIDStatusError)
}
