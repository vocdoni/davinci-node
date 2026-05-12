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

func TestAddProcessIDRegistersProcess(t *testing.T) {
	c := qt.New(t)
	pid := testutil.RandomProcessID()
	_, seq := newTestSequencer(t, createReadyProcess(t, pid))

	// Register the process with the sequencer
	seq.AddProcessID(pid)

	c.Assert(seq.ExistsProcessID(pid), qt.IsTrue)
}

func TestDelProcessIDRemovesProcess(t *testing.T) {
	c := qt.New(t)
	pid := testutil.RandomProcessID()
	_, seq := newTestSequencer(t, createReadyProcess(t, pid))

	// Register first
	seq.AddProcessID(pid)

	// Unregister
	seq.DelProcessID(pid)

	c.Assert(seq.ExistsProcessID(pid), qt.IsFalse)
}

func TestAddProcessIDIsIdempotent(t *testing.T) {
	c := qt.New(t)
	pid := testutil.RandomProcessID()
	_, seq := newTestSequencer(t, createReadyProcess(t, pid))

	seq.AddProcessID(pid)
	seq.AddProcessID(pid) // second add should be no-op

	// Verify in-memory state
	c.Assert(seq.ExistsProcessID(pid), qt.IsTrue)
}

func TestDelProcessIDIsIdempotent(t *testing.T) {
	c := qt.New(t)
	pid := testutil.RandomProcessID()
	_, seq := newTestSequencer(t, createReadyProcess(t, pid))

	seq.AddProcessID(pid)
	seq.DelProcessID(pid)
	seq.DelProcessID(pid) // second remove should be no-op

	// Verify in-memory state
	c.Assert(seq.ExistsProcessID(pid), qt.IsFalse)
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
	t.Cleanup(func() {
		stg.Close()
	})

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
// the status and time checks in IsAcceptingVotes.
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
