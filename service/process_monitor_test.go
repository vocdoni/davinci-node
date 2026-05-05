package service

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	censusDownloader := NewCensusDownloader(nil, store, CensusDownloaderConfig{
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
	monitor := NewProcessMonitor(contracts, defaultMockProcessIDVersion, store, censusDownloader, stateSync, time.Second*2)

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
		OrganizationID: contracts.AccountAddress(),
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
	censusDownloader.OnCensusDownloaded(processID, census, ctx, func(_ error) {
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

func TestProcessMonitorDoesNotCreateProcessWhenInitialCensusDownloadFails(t *testing.T) {
	c := qt.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := storage.New(memdb.New())
	c.Cleanup(store.Close)

	contracts := NewMockContracts()

	censusDownloader := NewCensusDownloader(nil, store, CensusDownloaderConfig{
		CleanUpInterval:      time.Minute,
		OnchainCheckInterval: time.Minute,
		Cooldown:             10 * time.Millisecond,
		Expiration:           time.Minute,
		Attempts:             5,
		AttemptTimeout:       time.Second,
		ConcurrentDownloads:  1,
	})
	c.Assert(censusDownloader.Start(ctx), qt.IsNil)
	c.Cleanup(censusDownloader.Stop)

	monitor := NewProcessMonitor(contracts, defaultMockProcessIDVersion, store, censusDownloader, nil, 10*time.Millisecond)
	c.Assert(monitor.Start(ctx), qt.IsNil)
	c.Cleanup(monitor.Stop)

	notFoundServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	c.Cleanup(notFoundServer.Close)

	process := testutil.RandomProcess(testutil.RandomProcessID())
	process.OrganizationID = contracts.AccountAddress()
	process.Census.CensusOrigin = types.CensusOriginMerkleTreeOffchainStaticV1
	process.Census.CensusURI = notFoundServer.URL
	process.Census.CensusRoot = types.HexBytes{0x04}

	processID, _, err := contracts.CreateProcess(process)
	c.Assert(err, qt.IsNil)

	time.Sleep(250 * time.Millisecond)

	_, err = store.Process(processID)
	c.Assert(err, qt.Equals, storage.ErrNotFound)
}

func TestProcessMonitorInitializeKnownProcessesRegistersOnlyMatchingVersion(t *testing.T) {
	c := qt.New(t)

	store := storage.New(memdb.New())
	c.Cleanup(store.Close)

	contracts := NewMockContracts()
	matchingProcessID := testMonitorProcessID(defaultMockProcessIDVersion, 1)
	foreignProcessID := testMonitorProcessID([4]byte{0xaa, 0xbb, 0xcc, 0xdd}, 2)

	matchingProcess := testutil.RandomProcess(matchingProcessID)
	matchingProcess.Status = types.ProcessStatusResults
	c.Assert(store.NewProcess(matchingProcess), qt.IsNil)

	foreignProcess := testutil.RandomProcess(foreignProcessID)
	foreignProcess.Status = types.ProcessStatusResults
	c.Assert(store.NewProcess(foreignProcess), qt.IsNil)

	monitor := NewProcessMonitor(contracts, defaultMockProcessIDVersion, store, nil, nil, time.Second)

	c.Assert(monitor.initializeKnownProcesses(), qt.IsNil)
	c.Assert(contracts.registeredKnownIDs, qt.DeepEquals, []types.ProcessID{matchingProcessID})
	c.Assert(contracts.processLookups, qt.HasLen, 0)
}

func TestProcessMonitorSyncActiveProcessesSkipsForeignVersion(t *testing.T) {
	c := qt.New(t)

	store := storage.New(memdb.New())
	c.Cleanup(store.Close)

	contracts := NewMockContracts()
	matchingProcessID := testMonitorProcessID(defaultMockProcessIDVersion, 3)
	foreignProcessID := testMonitorProcessID([4]byte{0xde, 0xad, 0xbe, 0xef}, 4)

	localMatchingProcess := testutil.RandomProcess(matchingProcessID)
	localMatchingProcess.VotersCount = types.NewInt(1)
	localMatchingProcess.OverwrittenVotesCount = types.NewInt(0)
	c.Assert(store.NewProcess(localMatchingProcess), qt.IsNil)

	remoteMatchingProcess := testutil.RandomProcess(matchingProcessID)
	remoteMatchingProcess.StateRoot = testutil.DeterministicStateRoot(20)
	remoteMatchingProcess.VotersCount = types.NewInt(5)
	remoteMatchingProcess.OverwrittenVotesCount = types.NewInt(2)
	contracts.processes = []*types.Process{remoteMatchingProcess}

	foreignProcess := testutil.RandomProcess(foreignProcessID)
	foreignProcess.VotersCount = types.NewInt(7)
	foreignProcess.OverwrittenVotesCount = types.NewInt(1)
	c.Assert(store.NewProcess(foreignProcess), qt.IsNil)

	monitor := NewProcessMonitor(contracts, defaultMockProcessIDVersion, store, nil, nil, time.Second)

	c.Assert(monitor.syncActiveProcessesFromBlockchain(), qt.IsNil)
	c.Assert(contracts.processLookups, qt.DeepEquals, []types.ProcessID{matchingProcessID})

	updatedMatchingProcess, err := store.Process(matchingProcessID)
	c.Assert(err, qt.IsNil)
	c.Assert(updatedMatchingProcess.StateRoot, qt.DeepEquals, remoteMatchingProcess.StateRoot)
	c.Assert(updatedMatchingProcess.VotersCount, qt.DeepEquals, remoteMatchingProcess.VotersCount)
	c.Assert(updatedMatchingProcess.OverwrittenVotesCount, qt.DeepEquals, remoteMatchingProcess.OverwrittenVotesCount)

	storedForeignProcess, err := store.Process(foreignProcessID)
	c.Assert(err, qt.IsNil)
	c.Assert(storedForeignProcess.StateRoot, qt.DeepEquals, foreignProcess.StateRoot)
	c.Assert(storedForeignProcess.VotersCount, qt.DeepEquals, foreignProcess.VotersCount)
	c.Assert(storedForeignProcess.OverwrittenVotesCount, qt.DeepEquals, foreignProcess.OverwrittenVotesCount)
}

func TestProcessMonitorIgnoresForeignRuntimeEvents(t *testing.T) {
	c := qt.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := storage.New(memdb.New())
	c.Cleanup(store.Close)

	contracts := NewMockContracts()
	monitor := NewProcessMonitor(contracts, defaultMockProcessIDVersion, store, nil, nil, time.Second)

	existingForeignProcessID := testMonitorProcessID([4]byte{0xde, 0xad, 0xbe, 0xef}, 5)
	existingForeignProcess := testutil.RandomProcess(existingForeignProcessID)
	existingForeignProcess.VotersCount = types.NewInt(3)
	existingForeignProcess.OverwrittenVotesCount = types.NewInt(1)
	c.Assert(store.NewProcess(existingForeignProcess), qt.IsNil)

	newForeignProcessID := testMonitorProcessID([4]byte{0xaa, 0xbb, 0xcc, 0xdd}, 6)
	newForeignProcess := testutil.RandomProcess(newForeignProcessID)

	newProcChan := make(chan *types.Process, 1)
	updatedProcChan := make(chan *types.ProcessWithChanges, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		monitor.monitorProcesses(ctx, newProcChan, updatedProcChan)
	}()

	newProcChan <- newForeignProcess
	updatedProcChan <- &types.ProcessWithChanges{
		ProcessID: existingForeignProcessID,
		StateRootChange: &types.StateRootChange{
			NewStateRoot:             testutil.DeterministicStateRoot(99),
			NewVotersCount:           types.NewInt(9),
			NewOverwrittenVotesCount: types.NewInt(4),
		},
	}

	time.Sleep(100 * time.Millisecond)
	cancel()
	close(newProcChan)
	close(updatedProcChan)
	<-done

	_, err := store.Process(newForeignProcessID)
	c.Assert(err, qt.Equals, storage.ErrNotFound)

	storedForeignProcess, err := store.Process(existingForeignProcessID)
	c.Assert(err, qt.IsNil)
	c.Assert(storedForeignProcess.StateRoot, qt.DeepEquals, existingForeignProcess.StateRoot)
	c.Assert(storedForeignProcess.VotersCount, qt.DeepEquals, existingForeignProcess.VotersCount)
	c.Assert(storedForeignProcess.OverwrittenVotesCount, qt.DeepEquals, existingForeignProcess.OverwrittenVotesCount)
}

func testMonitorProcessID(version [4]byte, nonce uint64) types.ProcessID {
	return types.NewProcessID(testutil.DeterministicAddress(nonce), version, nonce)
}
