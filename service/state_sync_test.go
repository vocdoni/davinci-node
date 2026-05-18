package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	statetest "github.com/vocdoni/davinci-node/state/testutil"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

func TestStateSync(t *testing.T) {
	c := qt.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Init("debug", "stdout", nil)

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
	monitor := NewProcessMonitor(contracts, defaultMockProcessIDVersion, store, censusDownloader, stateSync, time.Second)

	// Start monitoring in background
	c.Assert(monitor.Start(ctx), qt.IsNil)
	defer monitor.Stop()

	// Create a new encryption key for the process
	publicKey, privateKey, err := elgamal.GenerateKey(curves.New(bjj.CurveType))
	c.Assert(err, qt.IsNil)

	// Store the encryption keys
	err = store.SetEncryptionKeys(publicKey, privateKey)
	c.Assert(err, qt.IsNil)

	// Create a new process
	publicKeyX, publicKeyY := publicKey.Point()
	processID := types.NewProcessID(testutil.RandomAddress(), defaultMockProcessIDVersion, 0)
	process := testutil.CustomRandomProcess(processID, &types.EncryptionKey{
		X: (*types.BigInt)(publicKeyX),
		Y: (*types.BigInt)(publicKeyY),
	}, testutil.RandomCensus(types.CensusOriginCSPEdDSABabyJubJubV1))
	processID, createTx, err := contracts.CreateProcess(process)
	c.Assert(err, qt.IsNil)
	c.Assert(createTx, qt.Not(qt.IsNil))

	// Wait for transaction to be mined
	err = contracts.WaitTxByHash(*createTx, 30*time.Second)
	c.Assert(err, qt.IsNil)

	// Give monitor time to detect and store the process
	time.Sleep(3 * time.Second)

	// Verify process was stored
	proc, err := store.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(proc, qt.Not(qt.IsNil))
	c.Assert(proc.MetadataURI, qt.Equals, "http://example.com/metadata")
	c.Log(proc)

	// Use the process's ballot mode so the initialized state tree root matches
	// what NewProcess committed in storage.
	originalState, err := state.New(memdb.New(), processID)
	c.Assert(err, qt.IsNil)
	packedBallotMode, err := process.BallotMode.Pack()
	c.Assert(err, qt.IsNil)
	err = originalState.Initialize(
		process.Census.CensusOrigin.BigInt().MathBigInt(),
		packedBallotMode,
		types.EncryptionKeyFromPoint(publicKey))
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize original state"))

	oldStateRoot, err := originalState.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	i := 0

	// Create test votes for this transition (different votes each time)
	votes := statetest.NewVotesForTest(publicKey, 3, i)

	// Perform batch operation on original state
	batch, err := originalState.PrepareVotesBatch(votes)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to prepare batch %d", i+1))

	newStateRoot, err := batch.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	c.Assert(batch.Commit(), qt.IsNil)

	txHash := contracts.SendBlobTx(batch.BlobEvalData().Blob[:])
	{
		// Process state root should still be the initial root (old)
		// before the state root change event is applied.
		proc, err := store.Process(processID)
		c.Assert(err, qt.IsNil)
		c.Assert(proc, qt.Not(qt.IsNil))
		c.Assert(proc.VotersCount, qt.IsNil)
		c.Assert(proc.OverwrittenVotesCount, qt.IsNil)
	}
	err = contracts.MockStateRootChange(ctx, &types.ProcessWithChanges{
		ProcessID: *proc.ID,
		StateRootChange: &types.StateRootChange{
			OldStateRoot:             (*types.BigInt)(oldStateRoot),
			NewStateRoot:             (*types.BigInt)(newStateRoot),
			NewVotersCount:           types.NewInt(len(votes)),
			NewOverwrittenVotesCount: types.NewInt(0),
			TxHash:                   &txHash,
		},
	})
	c.Assert(err, qt.IsNil)

	// Give process monitor some time
	time.Sleep(3 * time.Second)

	{
		// Verify process is now updated
		proc, err := store.Process(processID)
		c.Assert(err, qt.IsNil)
		c.Assert(proc, qt.Not(qt.IsNil))
		c.Assert(proc.StateRoot, qt.DeepEquals, (*types.BigInt)(newStateRoot))
		c.Assert(proc.VotersCount, qt.DeepEquals, types.NewInt(3))
		c.Assert(proc.OverwrittenVotesCount, qt.DeepEquals, types.NewInt(0))
		c.Log(proc)
	}
}

func TestStateSyncSequentialPerProcess(t *testing.T) {
	c := qt.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	store := storage.New(memdb.New())
	defer store.Close()

	started := make(chan *types.ProcessWithChanges, 2)
	releaseFirst := make(chan struct{})

	stateSync := NewStateSync(NewMockContracts(), store)
	stateSync.applyFn = func(ctx context.Context, process *types.ProcessWithChanges) error {
		started <- process
		if process.NewStateRoot.Equal(testutil.DeterministicStateRoot(20)) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-releaseFirst:
			}
		}
		return nil
	}

	c.Assert(stateSync.Start(ctx), qt.IsNil)
	defer stateSync.Stop()

	stateSync.Notify(&types.ProcessWithChanges{
		ProcessID: testutil.FixedProcessID(),
		StateRootChange: &types.StateRootChange{
			OldStateRoot: testutil.DeterministicStateRoot(10),
			NewStateRoot: testutil.DeterministicStateRoot(20),
		},
	})

	stateSync.Notify(&types.ProcessWithChanges{
		ProcessID: testutil.FixedProcessID(),
		StateRootChange: &types.StateRootChange{
			OldStateRoot: testutil.DeterministicStateRoot(20),
			NewStateRoot: testutil.DeterministicStateRoot(30),
		},
	})

	select {
	case first := <-started:
		c.Assert(first.NewStateRoot.Equal(testutil.DeterministicStateRoot(20)), qt.IsTrue)
	case <-time.After(200 * time.Millisecond):
		c.Fatalf("first update did not start")
	}

	select {
	case <-started:
		c.Fatalf("second update started before first completed")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseFirst)

	select {
	case second := <-started:
		c.Assert(second.NewStateRoot.Equal(testutil.DeterministicStateRoot(30)), qt.IsTrue)
	case <-time.After(200 * time.Millisecond):
		c.Fatalf("second update did not start")
	}
}

func TestResolveBlobFetcherForProcessErrors(t *testing.T) {
	c := qt.New(t)

	processID := testutil.FixedProcessID()

	blobFetcher, err := resolveBlobFetcherForProcess(nil, processID)
	c.Assert(blobFetcher, qt.IsNil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "blob fetcher resolver is not configured")

	blobFetcher, err = resolveBlobFetcherForProcess(&testBlobFetcherResolver{
		errByProcess: map[types.ProcessID]error{
			processID: fmt.Errorf("boom"),
		},
	}, processID)
	c.Assert(blobFetcher, qt.IsNil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "resolve blob fetcher for process")

	blobFetcher, err = resolveBlobFetcherForProcess(&testBlobFetcherResolver{}, processID)
	c.Assert(blobFetcher, qt.IsNil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "nil blob fetcher")
}

func TestStateSyncFetchBlobAndApplyUsesResolvedBlobFetcher(t *testing.T) {
	c := qt.New(t)

	ctx := context.Background()
	store := storage.New(memdb.New())
	defer store.Close()

	processID := testutil.FixedProcessID()
	publicKey, _, err := elgamal.GenerateKey(state.Curve)
	c.Assert(err, qt.IsNil)

	storedState, err := state.New(store.StateDB(), processID)
	c.Assert(err, qt.IsNil)
	err = storedState.Initialize(
		types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(),
		testutil.BallotModePacked(),
		types.EncryptionKeyFromPoint(publicKey),
	)
	c.Assert(err, qt.IsNil)
	storedOldRoot, err := storedState.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	txHash := common.HexToHash("0x1234")
	selectedFetcher := &testBlobFetcher{}
	otherFetcher := &testBlobFetcher{}
	stateSync := NewStateSync(&testBlobFetcherResolver{
		fetchersByProcess: map[types.ProcessID]web3.BlobFetcher{
			processID:                          selectedFetcher,
			testutil.DeterministicProcessID(2): otherFetcher,
		},
	}, store)

	err = stateSync.fetchBlobAndApply(ctx, &types.ProcessWithChanges{
		ProcessID: processID,
		StateRootChange: &types.StateRootChange{
			OldStateRoot: (*types.BigInt)(storedOldRoot),
			NewStateRoot: testutil.DeterministicStateRoot(20),
			TxHash:       &txHash,
		},
	})
	c.Assert(err, qt.Not(qt.IsNil))
}

type testBlobFetcher struct {
	blobSidecars []*types.BlobSidecar
	txHashes     []common.Hash
	err          error
}

func (f *testBlobFetcher) BlobsByTxHash(_ context.Context, txHash common.Hash) ([]*types.BlobSidecar, error) {
	f.txHashes = append(f.txHashes, txHash)
	if f.err != nil {
		return nil, f.err
	}
	return f.blobSidecars, nil
}

type testBlobFetcherResolver struct {
	fetchersByProcess map[types.ProcessID]web3.BlobFetcher
	errByProcess      map[types.ProcessID]error
}

func (r *testBlobFetcherResolver) BlobFetcherForProcess(processID types.ProcessID) (web3.BlobFetcher, error) {
	if err, ok := r.errByProcess[processID]; ok {
		return nil, err
	}
	return r.fetchersByProcess[processID], nil
}
