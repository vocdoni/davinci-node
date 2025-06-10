package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/metadb"
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
	processID := &types.ProcessID{
		Address: common.Address{},
		Nonce:   42,
		ChainID: 1,
	}

	// Test 1: Get non-existent data
	metadata, err := st.Process(processID)
	c.Assert(err, qt.Equals, ErrNotFound)
	c.Assert(metadata, qt.IsNil)

	// Test 2: Set and get data
	testMetadata := &types.Metadata{
		Title:       map[string]string{"default": "Test Election"},
		Description: map[string]string{"default": "Test Description"},
		Media: types.MediaMetadata{
			Header: "header.jpg",
			Logo:   "logo.jpg",
		},
		Questions: []types.Question{
			{
				Title:       map[string]string{"default": "Question 1"},
				Description: map[string]string{"default": "Description 1"},
				Choices: []types.Choice{
					{
						Title: map[string]string{"default": "Choice 1"},
						Value: 0,
					},
					{
						Title: map[string]string{"default": "Choice 2"},
						Value: 1,
					},
				},
			},
		},
	}

	testProcess := &types.Process{
		ID:             processID.Marshal(),
		Status:         0,
		OrganizationId: common.Address{},
		StateRoot:      new(types.BigInt).SetUint64(100),
		StartTime:      time.Now(),
		Duration:       time.Hour,
		MetadataURI:    "https://example.com/metadata",
		BallotMode: &types.BallotMode{
			MaxCount:        2,
			MaxValue:        new(types.BigInt).SetUint64(100),
			MinValue:        new(types.BigInt).SetUint64(0),
			MaxTotalCost:    new(types.BigInt).SetUint64(0),
			MinTotalCost:    new(types.BigInt).SetUint64(0),
			ForceUniqueness: false,
			CostFromWeight:  false,
		},
		Census: &types.Census{
			CensusRoot:   make([]byte, 32),
			MaxVotes:     new(types.BigInt).SetUint64(100),
			CensusURI:    "https://example.com/census",
			CensusOrigin: 0,
		},
	}

	err = st.SetProcess(testProcess)
	c.Assert(err, qt.IsNil)

	// Get and verify data and metadata
	process, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(string(process.ID), qt.DeepEquals, string(processID.Marshal()))
	c.Assert(process.MetadataURI, qt.Equals, testProcess.MetadataURI)

	// Test 3: List processes
	processes, err := st.ListProcesses()
	c.Assert(err, qt.IsNil)
	c.Assert(len(processes), qt.Equals, 1)
	c.Assert(processes[0], qt.DeepEquals, processID.Marshal())

	// Test 4: Set another process
	anotherProcessID := types.ProcessID{
		Address: common.Address{1},
		Nonce:   43,
		ChainID: 1,
	}
	process.ID = anotherProcessID.Marshal()

	err = st.SetProcess(process)
	c.Assert(err, qt.IsNil)

	// Verify list now contains both processes
	processes, err = st.ListProcesses()
	c.Assert(err, qt.IsNil)
	c.Assert(len(processes), qt.Equals, 2)

	// Test 5: MetadataHash function
	hash1 := MetadataHash(testMetadata)
	c.Assert(hash1, qt.Not(qt.IsNil))
	c.Assert(len(hash1), qt.Equals, 32) // Ethereum hash length is 32 bytes

	// Modify metadata and verify hash changes
	testMetadata.Title["default"] = "Modified Title"
	hash2 := MetadataHash(testMetadata)
	c.Assert(hash2, qt.Not(qt.IsNil))
	c.Assert(hash2, qt.Not(qt.DeepEquals), hash1)
}
