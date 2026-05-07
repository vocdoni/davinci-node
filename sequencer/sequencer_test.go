package sequencer

import (
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	spechash "github.com/vocdoni/davinci-node/spec/hash"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

func TestAddProcessIDSetsRegisteredForSequencing(t *testing.T) {
	c := qt.New(t)
	pid := testutil.RandomProcessID()
	stg, seq := newTestSequencer(t, createReadyProcess(t, pid))

	// Pre-condition: process exists but is not registered for sequencing
	proc, err := stg.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(proc.RegisteredForSequencing, qt.IsFalse)

	// Register the process with the sequencer
	seq.AddProcessID(pid)

	// Verify storage was updated
	proc, err = stg.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(proc.RegisteredForSequencing, qt.IsTrue)
}

func TestDelProcessIDSetsRegisteredForSequencingFalse(t *testing.T) {
	c := qt.New(t)
	pid := testutil.RandomProcessID()
	stg, seq := newTestSequencer(t, createReadyProcess(t, pid))

	// Register first
	seq.AddProcessID(pid)
	proc, err := stg.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(proc.RegisteredForSequencing, qt.IsTrue)

	// Unregister
	seq.DelProcessID(pid)

	// Verify storage was updated
	proc, err = stg.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(proc.RegisteredForSequencing, qt.IsFalse)
}

func TestAddProcessIDIsIdempotent(t *testing.T) {
	c := qt.New(t)
	pid := testutil.RandomProcessID()
	stg, seq := newTestSequencer(t, createReadyProcess(t, pid))

	seq.AddProcessID(pid)
	seq.AddProcessID(pid) // second add should be no-op

	// Verify in-memory state
	c.Assert(seq.ExistsProcessID(pid), qt.IsTrue)

	// Verify storage state
	proc, err := stg.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(proc.RegisteredForSequencing, qt.IsTrue)
}

func TestDelProcessIDIsIdempotent(t *testing.T) {
	c := qt.New(t)
	pid := testutil.RandomProcessID()
	stg, seq := newTestSequencer(t, createReadyProcess(t, pid))

	seq.AddProcessID(pid)
	seq.DelProcessID(pid)
	seq.DelProcessID(pid) // second remove should be no-op

	// Verify in-memory state
	c.Assert(seq.ExistsProcessID(pid), qt.IsFalse)

	// Verify storage state
	proc, err := stg.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(proc.RegisteredForSequencing, qt.IsFalse)
}

func TestProcessIsAcceptingVotesRequiresSequencingRegistration(t *testing.T) {
	c := qt.New(t)
	pid := testutil.RandomProcessID()
	stg, seq := newTestSequencer(t, createReadyProcess(t, pid))

	// Process is ready but not registered for sequencing
	accepting, err := stg.ProcessIsAcceptingVotes(pid)
	c.Assert(accepting, qt.IsFalse)
	c.Assert(err, qt.ErrorMatches, ".*not registered for sequencing")

	// Register with sequencer
	seq.AddProcessID(pid)

	// Now it should accept votes
	accepting, err = stg.ProcessIsAcceptingVotes(pid)
	c.Assert(accepting, qt.IsTrue)
	c.Assert(err, qt.IsNil)
}

func TestProcessIsAcceptingVotesUnregisterAfterRegistration(t *testing.T) {
	c := qt.New(t)
	pid := testutil.RandomProcessID()
	stg, seq := newTestSequencer(t, createReadyProcess(t, pid))

	// Register
	seq.AddProcessID(pid)
	accepting, err := stg.ProcessIsAcceptingVotes(pid)
	c.Assert(accepting, qt.IsTrue)
	c.Assert(err, qt.IsNil)

	// Unregister
	seq.DelProcessID(pid)

	// No longer accepting votes
	accepting, err = stg.ProcessIsAcceptingVotes(pid)
	c.Assert(accepting, qt.IsFalse)
	c.Assert(err, qt.ErrorMatches, ".*not registered for sequencing")
}

func TestRegisteredForSequencingSurvivesStorageReload(t *testing.T) {
	c := qt.New(t)
	pid := testutil.RandomProcessID()
	stg, seq := newTestSequencer(t, createReadyProcess(t, pid))

	// Register with sequencer
	seq.AddProcessID(pid)

	// Reload from storage (simulating process restart)
	proc, err := stg.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(proc.RegisteredForSequencing, qt.IsTrue,
		qt.Commentf("RegisteredForSequencing must be persisted via CBOR so it survives restarts"))
}

func TestRegisteredForSequencingSerialization(t *testing.T) {
	c := qt.New(t)
	stg, _ := newTestSequencer(t, nil)
	pid := testutil.RandomProcessID()

	// Create a process with RegisteredForSequencing = true
	proc := createReadyProcess(t, pid)
	proc.RegisteredForSequencing = true
	c.Assert(stg.NewProcess(proc), qt.IsNil)

	// Reload and verify
	loaded, err := stg.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(loaded.RegisteredForSequencing, qt.IsTrue,
		qt.Commentf("CBOR serialization must preserve RegisteredForSequencing field"))
}

// newTestSequencer creates a storage and a minimal Sequencer suitable for
// testing AddProcessID / DelProcessID behaviour. The nil process argument
// allows callers that only need the storage without creating a process.
func newTestSequencer(t *testing.T, proc *types.Process) (*storage.Storage, *Sequencer) {
	t.Helper()
	dbDir := t.TempDir()
	testdb, err := metadb.New(db.TypePebble, dbDir)
	if err != nil {
		t.Fatalf("metadb.New: %v", err)
	}
	stg := storage.New(testdb)

	if proc != nil {
		if err := stg.NewProcess(proc); err != nil {
			t.Fatalf("NewProcess: %v", err)
		}
	}

	// Construct a minimal sequencer — only stg and processIDs are needed
	// for AddProcessID / DelProcessID. Full New() requires ZK artifacts.
	seq := &Sequencer{
		stg:        stg,
		processIDs: NewProcessIDMap(),
	}
	return stg, seq
}

// createReadyProcess creates a process with ProcessStatusReady so it passes
// the status check in ProcessIsAcceptingVotes.
func createReadyProcess(t *testing.T, pid types.ProcessID) *types.Process {
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

	return &types.Process{
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
}
