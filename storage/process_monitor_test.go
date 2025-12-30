package storage

import (
	"path/filepath"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/types"
)

func TestMonitorEndedProcesses(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create a process that should have ended 1 hour ago
	processID := testutil.DeterministicProcessID(42)

	pastTime := time.Now().Add(-2 * time.Hour) // Started 2 hours ago
	shortDuration := 1 * time.Hour             // Should have ended 1 hour ago

	process := &types.Process{
		ID:          &processID,
		Status:      types.ProcessStatusReady, // Still ready, should be ended
		StartTime:   pastTime,
		Duration:    shortDuration,
		MetadataURI: "http://example.com/metadata",
		StateRoot:   testutil.StateRoot(),
		BallotMode:  testutil.BallotModeInternal(),
		Census:      testutil.RandomCensus(types.CensusOriginMerkleTreeOffchainStaticV1),
	}

	// Store the process
	err = st.NewProcess(process)
	c.Assert(err, qt.IsNil)

	// Verify initial status
	p, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(p.Status, qt.Equals, types.ProcessStatusReady)

	// Manually call the check function (simulating what the monitor would do)
	st.checkAndUpdateEndedProcesses()

	// Verify the process status was updated to ended
	p, err = st.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(p.Status, qt.Equals, types.ProcessStatusEnded)
}

func TestMonitorEndedProcessesNotYetEnded(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create a process that should not have ended yet
	processID := testutil.DeterministicProcessID(43)

	recentTime := time.Now().Add(-30 * time.Minute) // Started 30 minutes ago
	longDuration := 2 * time.Hour                   // Should end in 1.5 hours

	process := &types.Process{
		ID:          &processID,
		Status:      types.ProcessStatusReady,
		StartTime:   recentTime,
		Duration:    longDuration,
		MetadataURI: "http://example.com/metadata",
		StateRoot:   testutil.StateRoot(),
		BallotMode:  testutil.BallotModeInternal(),
		Census:      testutil.RandomCensus(types.CensusOriginMerkleTreeOffchainStaticV1),
	}

	// Store the process
	err = st.NewProcess(process)
	c.Assert(err, qt.IsNil)

	// Verify initial status
	p, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(p.Status, qt.Equals, types.ProcessStatusReady)

	// Manually call the check function
	st.checkAndUpdateEndedProcesses()

	// Verify the process status remains ready (not ended yet)
	p, err = st.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(p.Status, qt.Equals, types.ProcessStatusReady)
}

func TestMonitorEndedProcessesSkipsAlreadyEnded(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create a process that is already ended
	processID := testutil.DeterministicProcessID(44)

	pastTime := time.Now().Add(-2 * time.Hour)
	shortDuration := 1 * time.Hour

	process := &types.Process{
		ID:          &processID,
		Status:      types.ProcessStatusEnded, // Already ended
		StartTime:   pastTime,
		Duration:    shortDuration,
		MetadataURI: "http://example.com/metadata",
		StateRoot:   testutil.StateRoot(),
		BallotMode:  testutil.BallotModeInternal(),
		Census:      testutil.RandomCensus(types.CensusOriginMerkleTreeOffchainStaticV1),
	}

	// Store the process
	err = st.NewProcess(process)
	c.Assert(err, qt.IsNil)

	// Verify initial status
	p, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(p.Status, qt.Equals, types.ProcessStatusEnded)

	// Manually call the check function
	st.checkAndUpdateEndedProcesses()

	// Verify the process status remains ended (no change)
	p, err = st.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(p.Status, qt.Equals, types.ProcessStatusEnded)
}
