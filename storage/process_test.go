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
		Version: []byte{0x00, 0x00, 0x00, 0x01},
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
		Meta: types.GenericMetadata{
			"testKey": 12,
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
			NumFields:      2,
			MaxValue:       new(types.BigInt).SetUint64(100),
			MinValue:       new(types.BigInt).SetUint64(0),
			MaxValueSum:    new(types.BigInt).SetUint64(0),
			MinValueSum:    new(types.BigInt).SetUint64(0),
			UniqueValues:   false,
			CostFromWeight: false,
		},
		Census: &types.Census{
			CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
			CensusRoot:   make([]byte, 32),
			CensusURI:    "https://example.com/census",
		},
	}

	err = st.NewProcess(testProcess)
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
	c.Assert(processes[0].Marshal(), qt.DeepEquals, processID.Marshal())

	// Test 4: Set another process
	anotherProcessID := types.ProcessID{
		Address: common.Address{1},
		Nonce:   43,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}
	process.ID = anotherProcessID.Marshal()

	err = st.NewProcess(process)
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
