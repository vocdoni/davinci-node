package storage

import (
	"path/filepath"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/internal/testutil"
)

func TestProcess(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create a test process ID
	processID := testutil.DeterministicProcessID(42)

	// Test 1: Get non-existent data
	metadata, err := st.Process(processID)
	c.Assert(err, qt.Equals, ErrNotFound)
	c.Assert(metadata, qt.IsNil)

	// Test 2: Check if the process exists
	exists, err := st.ProcessExists(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(exists, qt.IsFalse)

	testProcess := testutil.RandomProcess(processID)

	err = st.NewProcess(testProcess)
	c.Assert(err, qt.IsNil)

	// Get and verify data and metadata
	process, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(process.ID.Bytes(), qt.DeepEquals, processID.Bytes())
	c.Assert(process.MetadataURI, qt.Equals, testProcess.MetadataURI)

	// Test 3: List processes
	processes, err := st.ListProcesses()
	c.Assert(err, qt.IsNil)
	c.Assert(len(processes), qt.Equals, 1)
	c.Assert(processes[0].Bytes(), qt.DeepEquals, processID.Bytes())

	// Test 4: Set another process
	anotherProcessID := testutil.RandomProcessID()
	process = testutil.RandomProcess(anotherProcessID)

	err = st.NewProcess(process)
	c.Assert(err, qt.IsNil)

	// Verify list now contains both processes
	processes, err = st.ListProcesses()
	c.Assert(err, qt.IsNil)
	c.Assert(len(processes), qt.Equals, 2)

	// Close the db and try to check if an existing process exists
	st.Close()
	_, err = st.ProcessExists(processID)
	c.Assert(err, qt.IsNotNil)
}

func TestNewProcessRejectsNilCensus(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	process := testutil.RandomProcess(testutil.DeterministicProcessID(43))
	process.Census = nil

	err = st.NewProcess(process)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "no census provided")
}
