package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/metadb"
)

func TestProcessMonitor(t *testing.T) {
	c := qt.New(t)

	// Setup storage
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")
	database, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	store := storage.New(database)
	defer store.Close()

	// Setup mock web3 contracts
	contracts := NewMockContracts()

	// Create process monitor
	monitor := NewProcessMonitor(contracts, store, time.Second)

	// Start monitoring in background
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = monitor.Start(ctx)
	c.Assert(err, qt.IsNil)
	defer monitor.Stop()

	// Create a new process
	pid, hash, err := contracts.CreateProcess(&types.Process{
		Status:         0,
		OrganizationId: contracts.AccountAddress(),
		StateRoot:      new(types.BigInt).SetUint64(100),
		StartTime:      time.Now().Add(5 * time.Minute),
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
	})
	c.Assert(err, qt.IsNil)
	c.Assert(hash, qt.Not(qt.IsNil))

	// Wait for transaction to be mined
	err = contracts.WaitTx(*hash, 30*time.Second)
	c.Assert(err, qt.IsNil)

	// Give monitor time to detect and store the process
	time.Sleep(5 * time.Second)

	// Verify process was stored
	proc, err := store.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(proc, qt.Not(qt.IsNil))
	c.Assert(proc.MetadataURI, qt.Equals, "https://example.com/metadata")
}
