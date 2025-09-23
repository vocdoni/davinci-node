package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
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
	processID := &types.ProcessID{
		Address: common.Address{},
		Nonce:   42,
		Prefix:  []byte{0x00, 0x00, 0x00, 0x01},
	}

	pastTime := time.Now().Add(-2 * time.Hour) // Started 2 hours ago
	shortDuration := 1 * time.Hour             // Should have ended 1 hour ago

	process := &types.Process{
		ID:          processID.Marshal(),
		Status:      types.ProcessStatusReady, // Still ready, should be ended
		StartTime:   pastTime,
		Duration:    shortDuration,
		MetadataURI: "http://example.com/metadata",
		StateRoot:   new(types.BigInt).SetUint64(100),
		BallotMode: &types.BallotMode{
			NumFields:   8,
			MaxValue:    new(types.BigInt).SetUint64(100),
			MinValue:    new(types.BigInt).SetUint64(0),
			MaxValueSum: new(types.BigInt).SetUint64(0),
			MinValueSum: new(types.BigInt).SetUint64(0),
		},
		Census: &types.Census{
			CensusOrigin: types.CensusOriginMerkleTree,
			CensusRoot:   make([]byte, 32),
			MaxVotes:     new(types.BigInt).SetUint64(1000),
			CensusURI:    "http://example.com/census",
		},
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
	processID := &types.ProcessID{
		Address: common.Address{1},
		Nonce:   43,
		Prefix:  []byte{0x00, 0x00, 0x00, 0x01},
	}

	recentTime := time.Now().Add(-30 * time.Minute) // Started 30 minutes ago
	longDuration := 2 * time.Hour                   // Should end in 1.5 hours

	process := &types.Process{
		ID:          processID.Marshal(),
		Status:      types.ProcessStatusReady,
		StartTime:   recentTime,
		Duration:    longDuration,
		MetadataURI: "http://example.com/metadata",
		StateRoot:   new(types.BigInt).SetUint64(100),
		BallotMode: &types.BallotMode{
			NumFields:   8,
			MaxValue:    new(types.BigInt).SetUint64(100),
			MinValue:    new(types.BigInt).SetUint64(0),
			MaxValueSum: new(types.BigInt).SetUint64(0),
			MinValueSum: new(types.BigInt).SetUint64(0),
		},
		Census: &types.Census{
			CensusOrigin: types.CensusOriginMerkleTree,
			CensusRoot:   make([]byte, 32),
			MaxVotes:     new(types.BigInt).SetUint64(1000),
			CensusURI:    "http://example.com/census",
		},
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
	processID := &types.ProcessID{
		Address: common.Address{2},
		Nonce:   44,
		Prefix:  []byte{0x00, 0x00, 0x00, 0x01},
	}

	pastTime := time.Now().Add(-2 * time.Hour)
	shortDuration := 1 * time.Hour

	process := &types.Process{
		ID:          processID.Marshal(),
		Status:      types.ProcessStatusEnded, // Already ended
		StartTime:   pastTime,
		Duration:    shortDuration,
		MetadataURI: "http://example.com/metadata",
		StateRoot:   new(types.BigInt).SetUint64(100),
		BallotMode: &types.BallotMode{
			NumFields:   8,
			MaxValue:    new(types.BigInt).SetUint64(100),
			MinValue:    new(types.BigInt).SetUint64(0),
			MaxValueSum: new(types.BigInt).SetUint64(0),
			MinValueSum: new(types.BigInt).SetUint64(0),
		},
		Census: &types.Census{
			CensusOrigin: types.CensusOriginMerkleTree,
			CensusRoot:   make([]byte, 32),
			MaxVotes:     new(types.BigInt).SetUint64(1000),
			CensusURI:    "http://example.com/census",
		},
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
