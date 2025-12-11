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
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

func TestProcessMonitor(t *testing.T) {
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
		CleanUpInterval: 5 * time.Second,
		Cooldown:        5 * time.Second,
		Expiration:      30 * time.Minute,
		Attempts:        5,
	})
	c.Assert(censusDownloader.Start(ctx), qt.IsNil)
	c.Cleanup(censusDownloader.Stop)

	// Create process monitor
	monitor := NewProcessMonitor(contracts, store, censusDownloader, time.Second*2)

	// Start monitoring in background
	c.Assert(monitor.Start(ctx), qt.IsNil)
	defer monitor.Stop()

	// Create a new encryption key for the process
	publicKey, privateKey, err := elgamal.GenerateKey(curves.New(bjj.CurveType))
	c.Assert(err, qt.IsNil)

	// Create a new process
	pid, createTx, err := contracts.CreateProcess(&types.Process{
		Status:         types.ProcessStatusReady,
		OrganizationId: contracts.AccountAddress(),
		StateRoot:      new(types.BigInt).SetUint64(100),
		StartTime:      time.Now().Add(5 * time.Minute),
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
			CensusRoot:   make([]byte, 32),
			CensusURI:    "https://example.com/census",
			CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
		},
	})
	c.Assert(err, qt.IsNil)
	c.Assert(createTx, qt.Not(qt.IsNil))

	// Store the encryption keys for the process pid
	err = store.SetEncryptionKeys(pid, publicKey, privateKey)
	c.Assert(err, qt.IsNil)

	// Wait for transaction to be mined
	err = contracts.WaitTxByHash(*createTx, 30*time.Second)
	c.Assert(err, qt.IsNil)

	// Give monitor time to detect and store the process
	time.Sleep(3 * time.Second)

	// Verify process was stored
	proc, err := store.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(proc, qt.Not(qt.IsNil))
	c.Assert(proc.MetadataURI, qt.Equals, "https://example.com/metadata")
}
