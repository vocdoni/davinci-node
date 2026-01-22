package service

import (
	"context"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

func TestProcessMonitor(t *testing.T) {
	t.Skip("TODO: fix and re-enable")
	c := qt.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Setup storage
	store := storage.New(memdb.New())
	defer store.Close()

	// Setup mock web3 contracts
	contracts := NewMockContracts()

	// Setup census downloader
	censusDownloader := NewCensusDownloader(contracts, store, CensusDownloaderConfig{
		CleanUpInterval:      5 * time.Second,
		OnchainCheckInterval: time.Second * 5,
		Cooldown:             5 * time.Second,
		Expiration:           30 * time.Minute,
		Attempts:             5,
	})
	c.Assert(censusDownloader.Start(ctx), qt.IsNil)
	c.Cleanup(censusDownloader.Stop)

	// Start StateSync
	stateSync := NewStateSync(contracts, store)
	c.Assert(stateSync.Start(ctx), qt.IsNil)
	defer stateSync.Stop()

	// Create process monitor
	monitor := NewProcessMonitor(contracts, store, censusDownloader, stateSync, time.Second*2)

	// Start monitoring in background
	c.Assert(monitor.Start(ctx), qt.IsNil)
	defer monitor.Stop()

	// Create a new encryption key for the process
	publicKey, privateKey, err := elgamal.GenerateKey(curves.New(bjj.CurveType))
	c.Assert(err, qt.IsNil)

	// Create a new census
	census := &types.Census{
		CensusRoot:   make([]byte, 32),
		CensusURI:    "https://example.com/census",
		CensusOrigin: types.CensusOriginCSPEdDSABabyJubJubV1,
	}

	// Create a new process
	processID, createTx, err := contracts.CreateProcess(&types.Process{
		Status:         types.ProcessStatusReady,
		OrganizationId: contracts.AccountAddress(),
		StateRoot:      testutil.FixedStateRoot(),
		StartTime:      time.Now().Add(5 * time.Minute),
		Duration:       time.Hour,
		MetadataURI:    "https://example.com/metadata",
		BallotMode:     testutil.BallotMode(),
		Census:         census,
	})
	c.Assert(err, qt.IsNil)
	c.Assert(createTx, qt.Not(qt.IsNil))

	// Store the encryption keys for the process id
	t.Log("TODO: fix SetEncryptionKeys", processID, publicKey, privateKey)
	// err = store.SetEncryptionKeys(processID, publicKey, privateKey)
	// c.Assert(err, qt.IsNil)

	// Wait for transaction to be mined
	err = contracts.WaitTxByHash(*createTx, 30*time.Second)
	c.Assert(err, qt.IsNil)

	// Allow some time for the monitor to pick up the new process
	time.Sleep(3 * time.Second)

	// Create a wait group for census download
	censusDownloaded := make(chan struct{})

	// Register a callback for when the census is downloaded
	censusDownloader.OnCensusDownloaded(census, ctx, func(_ error) {
		// Discard here error if any (csp censuses will not be downloaded, even in the pending downloads list)
		close(censusDownloaded)
	})

	// Wait for the census to be downloaded
	<-censusDownloaded

	// Verify process was stored
	proc, err := store.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(proc, qt.Not(qt.IsNil))
	c.Assert(proc.MetadataURI, qt.Equals, "https://example.com/metadata")
}
