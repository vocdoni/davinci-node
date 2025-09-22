package sequencer

import (
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits/results"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

func loadResultsVerifierArtifactsForTest(t *testing.T) *internalCircuits {
	t.Helper()
	ca := new(internalCircuits)
	err := results.Artifacts.DownloadAll(t.Context())
	qt.Assert(t, err, qt.IsNil, qt.Commentf("failed to download results verifier artifacts: %v", err))
	ca.rvCcs, ca.rvPk, err = loadCircuitArtifacts(results.Artifacts)
	qt.Assert(t, err, qt.IsNil, qt.Commentf("failed to load results verifier artifacts: %v", err))
	return ca
}

// TestFinalize tests the finalize method of the Finalizer struct
func TestFinalize(t *testing.T) {
	c := qt.New(t)

	// Setup test environment
	stg, stateDB, pid, _, _, _, cleanup := setupTestEnvironment(t, 10000, 5000)
	defer cleanup()

	// Create a finalizer
	f := newFinalizer(stg, stateDB, loadResultsVerifierArtifactsForTest(t), nil)
	f.Start(t.Context(), 0)

	// Test finalize
	f.OndemandCh <- pid
	_, err := f.WaitUntilResults(t.Context(), pid)
	c.Assert(err, qt.IsNil, qt.Commentf("finalize failed: %v", err))

	// Check that the process has been updated with the result
	process, err := stg.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(process.Result, qt.Not(qt.IsNil))

	// Verify the results are as expected
	c.Assert(len(process.Result), qt.Equals, types.FieldsPerBallot)
	expected := big.NewInt(5000)
	c.Assert(process.Result[0].MathBigInt().Cmp(expected), qt.Equals, 0,
		qt.Commentf("Expected first result to be 500, got %s", process.Result[0].String()))
}

// setupTestEnvironment creates a test environment with necessary objects
func setupTestEnvironment(t *testing.T, addValue, subValue int64) (
	*storage.Storage,
	db.Database,
	*types.ProcessID,
	ecc.Point,
	ecc.Point,
	*big.Int,
	func(),
) {
	// Create temporary directory
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	// Create database
	mainDB, err := metadb.New(db.TypePebble, dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	// Create storage
	stg := storage.New(mainDB)

	// Get state database
	stateDB := stg.StateDB()

	// Create a process ID
	pid := &types.ProcessID{
		Address: common.Address{},
		Nonce:   42,
		ChainID: 1,
	}

	// Create encryption keys
	curve := curves.New(bjj.CurveType)
	pubKey, privKey, err := elgamal.GenerateKey(curve)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Store the keys in storage
	err = stg.SetEncryptionKeys(pid, pubKey, privKey)
	if err != nil {
		t.Fatalf("failed to store encryption keys: %v", err)
	}
	// Create a process
	process := &types.Process{
		ID:          pid.Marshal(),
		Status:      0,
		StartTime:   time.Now(),
		Duration:    time.Hour,
		MetadataURI: "http://example.com/metadata",
		StateRoot:   new(types.BigInt).SetUint64(100),
		BallotMode: &types.BallotMode{
			NumFields:      8,
			MaxValue:       new(types.BigInt).SetUint64(100),
			MinValue:       new(types.BigInt).SetUint64(0),
			MaxValueSum:    new(types.BigInt).SetUint64(0),
			MinValueSum:    new(types.BigInt).SetUint64(0),
			UniqueValues:   false,
			CostFromWeight: false,
		},
		Census: &types.Census{
			CensusRoot:   make([]byte, 32),
			MaxVotes:     &types.BigInt{},
			CensusURI:    "http://example.com/census",
			CensusOrigin: types.CensusOriginMerkleTree,
		},
	}

	// Set BigInt values
	process.Census.MaxVotes.SetUint64(1000)

	// Store the process
	err = stg.NewProcess(process)
	if err != nil {
		t.Fatalf("failed to store process: %v", err)
	}

	process, err = stg.Process(pid)
	if err != nil {
		t.Fatalf("failed to get process: %v", err)
	}

	// Setup state with test data
	process.StateRoot = setupTestState(t, stateDB, pid, pubKey, process.StateRoot.MathBigInt(), addValue, subValue)
	err = stg.UpdateProcess(pid.Marshal(), func(p *types.Process) error {
		p.StateRoot = process.StateRoot
		return nil
	})
	if err != nil {
		t.Fatalf("failed to store process: %v", err)
	}

	// Return cleanup function
	cleanup := func() {
		stg.Close()
	}

	return stg, stateDB, pid, curve, pubKey, privKey, cleanup
}

// setupTestState initializes the state with encrypted test data
func setupTestState(
	t *testing.T,
	stateDB db.Database,
	pid *types.ProcessID,
	pubKey ecc.Point,
	stateRoot *big.Int,
	addValue, subValue int64,
) *types.BigInt {
	// Load the initial state
	st, err := state.LoadOnRoot(stateDB, pid.BigInt(), stateRoot)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	// Create encrypted accumulators with known values
	curve := pubKey.New()

	// Add accumulator with a known value
	addAccumulator := elgamal.NewBallot(curve)
	addValues := [types.FieldsPerBallot]*big.Int{}
	// Set values for testing
	for i := range types.FieldsPerBallot {
		addValues[i] = big.NewInt(addValue)
	}
	// Encrypt the values
	k1 := new(big.Int).SetBytes(util.RandomBytes(16))
	encryptedAdd, err := addAccumulator.Encrypt(addValues, pubKey, k1)
	if err != nil {
		t.Fatalf("failed to encrypt add accumulator: %v", err)
	}

	// Sub accumulator with a known value
	subAccumulator := elgamal.NewBallot(curve)
	subValues := [types.FieldsPerBallot]*big.Int{}
	// Set values for testing
	for i := range types.FieldsPerBallot {
		subValues[i] = big.NewInt(subValue)
	}
	// Encrypt the values
	k2 := new(big.Int).SetBytes(util.RandomBytes(16))
	encryptedSub, err := subAccumulator.Encrypt(subValues, pubKey, k2)
	if err != nil {
		t.Fatalf("failed to encrypt sub accumulator: %v", err)
	}

	// Store the encrypted accumulators in the state
	st.SetResultsAdd(encryptedAdd)
	st.SetResultsSub(encryptedSub)

	stateRoot, err = st.RootAsBigInt()
	if err != nil {
		t.Fatalf("failed to get state root: %v", err)
	}
	return (*types.BigInt)(stateRoot)
}

// TestFinalize_CleansStorage ensures that, after finalization, any pending
// artifacts in storage for the process are removed (pending ballots, verified
// ballots, aggregator batches, state transition batches).
func TestFinalize_CleansStorage(t *testing.T) {
	c := qt.New(t)

	// Setup environment and process ready for finalization
	stg, stateDB, pid, _, _, _, cleanup := setupTestEnvironment(t, 10000, 5000)
	defer cleanup()

	// Seed storage with various pending artifacts for this process
	pidBytes := pid.Marshal()

	// 1) Verified ballots (push pending -> reserve -> mark done)
	verifiedIDs := [][]byte{[]byte("vv1")}
	for _, id := range verifiedIDs {
		err := stg.PushBallot(&storage.Ballot{
			ProcessID: types.HexBytes(pidBytes),
			VoteID:    types.HexBytes(id),
		})
		if err != nil {
			t.Fatalf("PushBallot for verified(%x): %v", id, err)
		}
		_, key, err := stg.NextBallot()
		if err != nil {
			t.Fatalf("NextBallot for verified(%x): %v", id, err)
		}
		err = stg.MarkBallotDone(key, &storage.VerifiedBallot{
			ProcessID: types.HexBytes(pidBytes),
			VoteID:    types.HexBytes(id),
		})
		if err != nil {
			t.Fatalf("MarkBallotDone for verified(%x): %v", id, err)
		}
	}

	// 2) Pending ballots
	pendingIDs := [][]byte{[]byte("pv1"), []byte("pv2")}
	for _, id := range pendingIDs {
		err := stg.PushBallot(&storage.Ballot{
			ProcessID: types.HexBytes(pidBytes),
			VoteID:    types.HexBytes(id),
		})
		if err != nil {
			t.Fatalf("PushBallot(%x): %v", id, err)
		}
	}

	// 3) Aggregator batch (ready for state transition)
	aggIDs := [][]byte{[]byte("ab1")}
	aggBallots := make([]*storage.AggregatorBallot, 0, len(aggIDs))
	for _, id := range aggIDs {
		aggBallots = append(aggBallots, &storage.AggregatorBallot{
			VoteID: types.HexBytes(id),
		})
	}
	err := stg.PushBallotBatch(&storage.AggregatorBallotBatch{
		ProcessID: types.HexBytes(pidBytes),
		Ballots:   aggBallots,
	})
	if err != nil {
		t.Fatalf("PushBallotBatch: %v", err)
	}

	// 4) State transition batch (pending state transitions)
	stIDs := [][]byte{[]byte("st1"), []byte("st2")}
	stBallots := make([]*storage.AggregatorBallot, 0, len(stIDs))
	for _, id := range stIDs {
		stBallots = append(stBallots, &storage.AggregatorBallot{
			VoteID: types.HexBytes(id),
		})
	}
	err = stg.PushStateTransitionBatch(&storage.StateTransitionBatch{
		ProcessID: types.HexBytes(pidBytes),
		Ballots:   stBallots,
	})
	if err != nil {
		t.Fatalf("PushStateTransitionBatch: %v", err)
	}

	// Sanity: there should be pending and verified elements before finalize
	c.Assert(stg.CountPendingBallots() >= 1, qt.IsTrue)
	c.Assert(stg.CountVerifiedBallots(pidBytes) >= 1, qt.IsTrue)

	// Sanity: aggregator and state transition batches exist (do not reserve them)
	// We avoid creating reservations to keep cleanup straightforward

	// Create and start finalizer
	f := newFinalizer(stg, stateDB, loadResultsVerifierArtifactsForTest(t), nil)
	f.Start(t.Context(), 0)

	// Trigger finalization and wait for results
	f.OndemandCh <- pid
	_, err = f.WaitUntilResults(t.Context(), pid)
	c.Assert(err, qt.IsNil, qt.Commentf("finalize failed: %v", err))

	// Wait until cleanup is done (finalizer sets results before cleanup runs)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for storage cleanup after finalize")
		}
		pending := stg.CountPendingBallots()
		verified := stg.CountVerifiedBallots(pidBytes)
		_, _, errBatch := stg.NextBallotBatch(pidBytes)
		_, _, errST := stg.NextStateTransitionBatch(pidBytes)

		batchesGone := (errBatch == storage.ErrNoMoreElements)
		stGone := (errST == storage.ErrNoMoreElements)

		if pending == 0 && verified == 0 && batchesGone && stGone {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// After finalize, storage should be cleaned for this process
	c.Assert(stg.CountPendingBallots(), qt.Equals, 0)

	// Verified queue should have no available (non-reserved) items
	// PullVerifiedBallots returns ErrNotFound if none available (reserved are skipped)
	if _, _, err := stg.PullVerifiedBallots(pidBytes, 10); err == nil {
		t.Fatalf("expected no verified ballots available after finalize")
	} else {
		if err != storage.ErrNotFound {
			t.Fatalf("expected ErrNotFound for verified ballots, got: %v", err)
		}
	}

	// Aggregator batches should be gone
	if _, _, err := stg.NextBallotBatch(pidBytes); err == nil {
		t.Fatalf("expected no more aggregator batches after finalize")
	} else {
		// Prefer exact error comparison to avoid extra imports
		if err != storage.ErrNoMoreElements {
			t.Fatalf("expected ErrNoMoreElements for aggregator batches, got: %v", err)
		}
	}

	// State transition batches should be gone
	if _, _, err := stg.NextStateTransitionBatch(pidBytes); err == nil {
		t.Fatalf("expected no more state transition batches after finalize")
	} else {
		if err != storage.ErrNoMoreElements {
			t.Fatalf("expected ErrNoMoreElements for state transition batches, got: %v", err)
		}
	}

	// Verified results should have been stored
	c.Assert(stg.HasVerifiedResults(pidBytes), qt.IsTrue)
}
