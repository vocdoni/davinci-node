package storage

import (
	"errors"
	"fmt"
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/types"
)

// TestHasVerifiedResults tests the HasVerifiedResults functionality
func TestHasVerifiedResults(t *testing.T) {
	c := qt.New(t)

	// Create a test storage instance
	dbDir := t.TempDir()
	testdb, err := metadb.New(db.TypePebble, dbDir)
	c.Assert(err, qt.IsNil)

	// Create storage instance
	stg := New(testdb)
	defer stg.Close()

	// Test process ID
	processID := []byte("test-process-123")

	// Test 1: Check non-existent results
	hasResults := stg.HasVerifiedResults(processID)
	c.Assert(hasResults, qt.IsFalse, qt.Commentf("expected no results for new process"))

	// Test 2: Push verified results
	results := &VerifiedResults{
		ProcessID: types.HexBytes(processID),
		Inputs: ResultsVerifierProofInputs{
			StateRoot: big.NewInt(12345),
			Results: [types.FieldsPerBallot]*big.Int{
				big.NewInt(100),
				big.NewInt(200),
				big.NewInt(300),
				big.NewInt(400),
			},
		},
		// Proof would be set in real usage, but not needed for this test
	}
	err = stg.PushVerifiedResults(results)
	c.Assert(err, qt.IsNil)

	// Test 3: Check that results now exist
	hasResults = stg.HasVerifiedResults(processID)
	c.Assert(hasResults, qt.IsTrue, qt.Commentf("expected results to exist after push"))

	// Test 4: Check NextVerifiedResults still returns the results
	nextResults, err := stg.NextVerifiedResults()
	c.Assert(err, qt.IsNil)
	c.Assert([]byte(nextResults.ProcessID), qt.DeepEquals, processID)

	// Test 5: Mark results as done
	err = stg.MarkVerifiedResultsDone(processID)
	c.Assert(err, qt.IsNil)

	// Test 6: Check that results no longer exist after marking done
	hasResults = stg.HasVerifiedResults(processID)
	c.Assert(hasResults, qt.IsFalse, qt.Commentf("expected no results after marking done"))

	// Test 7: Try pushing duplicate results (should fail)
	err = stg.PushVerifiedResults(results)
	c.Assert(err, qt.IsNil) // First push after deletion should succeed

	err = stg.PushVerifiedResults(results)
	c.Assert(err, qt.Not(qt.IsNil), qt.Commentf("expected error when pushing duplicate results"))
	c.Assert(err.Error(), qt.Contains, "already exists")
}

// TestHasVerifiedResultsConcurrency tests concurrent access to HasVerifiedResults
func TestHasVerifiedResultsConcurrency(t *testing.T) {
	c := qt.New(t)

	// Create a test storage instance
	dbDir := t.TempDir()
	testdb, err := metadb.New(db.TypePebble, dbDir)
	c.Assert(err, qt.IsNil)

	// Create storage instance
	stg := New(testdb)
	defer stg.Close()

	// Test with multiple processes
	numProcesses := 10
	numGoroutines := 5

	// Create and push results for multiple processes
	for i := range numProcesses {
		processID := fmt.Appendf(nil, "process-%d", i)
		results := &VerifiedResults{
			ProcessID: types.HexBytes(processID),
			Inputs: ResultsVerifierProofInputs{
				StateRoot: big.NewInt(int64(i)),
				Results: [types.FieldsPerBallot]*big.Int{
					big.NewInt(int64(i * 100)),
					big.NewInt(int64(i * 200)),
					big.NewInt(int64(i * 300)),
					big.NewInt(int64(i * 400)),
				},
			},
		}
		err := stg.PushVerifiedResults(results)
		c.Assert(err, qt.IsNil)
	}

	// Concurrently check for results
	done := make(chan bool)
	for i := range numGoroutines {
		go func(workerID int) {
			defer func() { done <- true }()

			for j := range numProcesses {
				processID := fmt.Appendf(nil, "process-%d", j)
				if !stg.HasVerifiedResults(processID) {
					t.Errorf("worker %d: expected results for process %d", workerID, j)
					return
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for range numGoroutines {
		<-done
	}

	// Verify all results still exist
	for i := range numProcesses {
		processID := fmt.Appendf(nil, "process-%d", i)
		hasResults := stg.HasVerifiedResults(processID)
		c.Assert(hasResults, qt.IsTrue, qt.Commentf("expected results for process %d", i))
	}
}

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	dbDir := t.TempDir()
	testdb, err := metadb.New(db.TypePebble, dbDir)
	if err != nil {
		t.Fatalf("metadb.New: %v", err)
	}
	return New(testdb)
}

func mkBallot(pid, id []byte) *Ballot {
	return &Ballot{
		ProcessID: types.HexBytes(pid),
		VoteID:    types.HexBytes(id),
	}
}

func mkVerifiedBallot(pid, id []byte) *VerifiedBallot {
	return &VerifiedBallot{
		ProcessID: types.HexBytes(pid),
		VoteID:    types.HexBytes(id),
	}
}

func mkAggBallot(id []byte) *AggregatorBallot {
	return &AggregatorBallot{
		VoteID: types.HexBytes(id),
	}
}

func ensureProcess(t *testing.T, stg *Storage, pid []byte) {
	t.Helper()
	bm := &types.BallotMode{
		NumFields:    uint8(types.FieldsPerBallot),
		UniqueValues: false,
		MaxValue:     types.NewInt(1000),
		MinValue:     types.NewInt(0),
		MaxValueSum:  types.NewInt(1000),
		MinValueSum:  types.NewInt(0),
		CostExponent: 0,
	}
	censusRoot := make([]byte, types.CensusRootLength)
	proc := &types.Process{
		ID:         types.HexBytes(pid),
		Status:     types.ProcessStatusReady,
		BallotMode: bm,
		Census: &types.Census{
			CensusOrigin: types.CensusOriginMerkleTree,
			CensusRoot:   types.HexBytes(censusRoot),
		},
	}
	if err := stg.NewProcess(proc); err != nil {
		t.Fatalf("NewProcess(%x): %v", pid, err)
	}
}

func TestBallotQueue_RemoveBallot(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := []byte("p1")
	id1 := []byte("id1")
	ensureProcess(t, stg, pid)

	// push a pending ballot
	err := stg.PushBallot(mkBallot(pid, id1))
	c.Assert(err, qt.IsNil)

	// remove it
	err = stg.RemoveBallot(pid, id1)
	c.Assert(err, qt.IsNil)

	// no more pending ballots
	_, _, err = stg.NextBallot()
	c.Assert(errors.Is(err, ErrNoMoreElements), qt.IsTrue)

	// status should be error
	status, err := stg.VoteIDStatus(pid, id1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusError)
}

func TestBallotQueue_RemovePendingBallotsByProcess_RemovesAllPendingCurrently(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid1 := []byte("p1")
	pid2 := []byte("p2")
	ids := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	ensureProcess(t, stg, pid1)
	ensureProcess(t, stg, pid2)

	c.Assert(stg.PushBallot(mkBallot(pid1, ids[0])), qt.IsNil)
	c.Assert(stg.PushBallot(mkBallot(pid1, ids[1])), qt.IsNil)
	c.Assert(stg.PushBallot(mkBallot(pid2, ids[2])), qt.IsNil)

	// remove pending ballots "by process" (but currently removes all)
	err := stg.RemovePendingBallotsByProcess(pid1)
	c.Assert(err, qt.IsNil)

	// no pending should remain
	_, _, err = stg.NextBallot()
	c.Assert(errors.Is(err, ErrNoMoreElements), qt.IsTrue)
	c.Assert(stg.CountPendingBallots(), qt.Equals, 0)
}

func TestBallotQueue_ReleaseBallotReservation(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := []byte("p1")
	id1 := []byte("id1")
	ensureProcess(t, stg, pid)

	c.Assert(stg.PushBallot(mkBallot(pid, id1)), qt.IsNil)
	c.Assert(stg.CountPendingBallots(), qt.Equals, 1)

	// reserve the ballot
	_, key, err := stg.NextBallot()
	c.Assert(err, qt.IsNil)
	c.Assert(stg.CountPendingBallots(), qt.Equals, 0)

	// release the reservation
	err = stg.ReleaseBallotReservation(key)
	c.Assert(err, qt.IsNil)
	c.Assert(stg.CountPendingBallots(), qt.Equals, 1)
}

func TestBallotQueue_MarkBallotDoneAndPullVerified(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := []byte("p1")
	id1 := []byte("id1")
	ensureProcess(t, stg, pid)

	c.Assert(stg.PushBallot(mkBallot(pid, id1)), qt.IsNil)

	// reserve pending ballot
	_, key, err := stg.NextBallot()
	c.Assert(err, qt.IsNil)

	// mark done - move to verified queue
	vb := mkVerifiedBallot(pid, id1)
	err = stg.MarkBallotDone(key, vb)
	c.Assert(err, qt.IsNil)

	// pending queue empty
	c.Assert(stg.CountPendingBallots(), qt.Equals, 0)

	// verified queue contains it
	c.Assert(stg.CountVerifiedBallots(pid), qt.Equals, 1)

	// pull verified (creates reservation)
	vbs, keys, err := stg.PullVerifiedBallots(pid, 1)
	c.Assert(err, qt.IsNil)
	c.Assert(len(vbs), qt.Equals, 1)
	c.Assert(len(keys), qt.Equals, 1)
	c.Assert([]byte(vbs[0].ProcessID), qt.DeepEquals, pid)
	c.Assert([]byte(vbs[0].VoteID), qt.DeepEquals, id1)

	// status should be verified
	status, err := stg.VoteIDStatus(pid, id1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusVerified)

	// mark verified done - remove from verified queue
	err = stg.MarkVerifiedBallotsDone(keys...)
	c.Assert(err, qt.IsNil)
	c.Assert(stg.CountVerifiedBallots(pid), qt.Equals, 0)
}

func TestBallotQueue_PullVerifiedBallots_ReservationsAndLimits(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := []byte("p1")
	ids := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	ensureProcess(t, stg, pid)

	// create 3 verified ballots via Push + Next + MarkDone
	for _, id := range ids {
		c.Assert(stg.PushBallot(mkBallot(pid, id)), qt.IsNil)
		_, key, err := stg.NextBallot()
		c.Assert(err, qt.IsNil)
		c.Assert(stg.MarkBallotDone(key, mkVerifiedBallot(pid, id)), qt.IsNil)
	}

	// pull 2 (reserve them)
	vbs, keys, err := stg.PullVerifiedBallots(pid, 2)
	c.Assert(err, qt.IsNil)
	c.Assert(len(vbs), qt.Equals, 2)
	c.Assert(len(keys), qt.Equals, 2)

	// count excludes reserved
	c.Assert(stg.CountVerifiedBallots(pid), qt.Equals, 1)

	// subsequent pull should return the remaining one
	vbs2, keys2, err := stg.PullVerifiedBallots(pid, 5)
	c.Assert(err, qt.IsNil)
	c.Assert(len(vbs2), qt.Equals, 1)
	c.Assert(len(keys2), qt.Equals, 1)
}

func TestBallotQueue_RemoveVerifiedBallotsByProcess(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid1 := []byte("p1")
	pid2 := []byte("p2")
	ensureProcess(t, stg, pid1)
	ensureProcess(t, stg, pid2)

	// create verified for pid1 and pid2
	for i := 0; i < 2; i++ {
		id := []byte(fmt.Sprintf("a%d", i))
		c.Assert(stg.PushBallot(mkBallot(pid1, id)), qt.IsNil)
		_, key, err := stg.NextBallot()
		c.Assert(err, qt.IsNil)
		c.Assert(stg.MarkBallotDone(key, mkVerifiedBallot(pid1, id)), qt.IsNil)
	}
	for i := 0; i < 1; i++ {
		id := []byte(fmt.Sprintf("b%d", i))
		c.Assert(stg.PushBallot(mkBallot(pid2, id)), qt.IsNil)
		_, key, err := stg.NextBallot()
		c.Assert(err, qt.IsNil)
		c.Assert(stg.MarkBallotDone(key, mkVerifiedBallot(pid2, id)), qt.IsNil)
	}

	c.Assert(stg.CountVerifiedBallots(pid1), qt.Equals, 2)
	c.Assert(stg.CountVerifiedBallots(pid2), qt.Equals, 1)

	// remove only pid1
	err := stg.RemoveVerifiedBallotsByProcess(pid1)
	c.Assert(err, qt.IsNil)

	c.Assert(stg.CountVerifiedBallots(pid1), qt.Equals, 0)
	c.Assert(stg.CountVerifiedBallots(pid2), qt.Equals, 1)
}

func TestBallotQueue_MarkVerifiedBallotsFailed(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := []byte("p1")
	ids := [][]byte{[]byte("a"), []byte("b")}
	ensureProcess(t, stg, pid)

	// create verified ballots
	for _, id := range ids {
		c.Assert(stg.PushBallot(mkBallot(pid, id)), qt.IsNil)
		_, key, err := stg.NextBallot()
		c.Assert(err, qt.IsNil)
		c.Assert(stg.MarkBallotDone(key, mkVerifiedBallot(pid, id)), qt.IsNil)
	}

	// pull to get keys (and reserve)
	vbs, keys, err := stg.PullVerifiedBallots(pid, 10)
	c.Assert(err, qt.IsNil)
	c.Assert(len(vbs), qt.Equals, 2)
	c.Assert(len(keys), qt.Equals, 2)

	// mark as failed
	err = stg.MarkVerifiedBallotsFailed(keys...)
	c.Assert(err, qt.IsNil)

	// no verified left
	c.Assert(stg.CountVerifiedBallots(pid), qt.Equals, 0)

	// statuses should be error
	for _, id := range ids {
		status, err := stg.VoteIDStatus(pid, id)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, VoteIDStatusError)
	}
}

func TestBallotQueue_RemoveBallotBatchesByProcess(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid1 := []byte("p1")
	pid2 := []byte("p2")
	ensureProcess(t, stg, pid1)
	ensureProcess(t, stg, pid2)

	// push two batches: one per process
	batch1 := &AggregatorBallotBatch{
		ProcessID: types.HexBytes(pid1),
		Ballots: []*AggregatorBallot{
			mkAggBallot([]byte("a1")),
		},
	}
	batch2 := &AggregatorBallotBatch{
		ProcessID: types.HexBytes(pid2),
		Ballots: []*AggregatorBallot{
			mkAggBallot([]byte("b1")),
		},
	}

	c.Assert(stg.PushBallotBatch(batch1), qt.IsNil)
	c.Assert(stg.PushBallotBatch(batch2), qt.IsNil)

	// remove batches for pid1
	err := stg.RemoveBallotBatchesByProcess(pid1)
	c.Assert(err, qt.IsNil)

	// no batch for pid1
	_, _, err = stg.NextBallotBatch(pid1)
	c.Assert(errors.Is(err, ErrNoMoreElements), qt.IsTrue)

	// but pid2 still available
	_, _, err = stg.NextBallotBatch(pid2)
	c.Assert(err, qt.IsNil)
}

func TestBallotQueue_MarkBallotBatchFailed(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := []byte("p1")
	ids := [][]byte{[]byte("a"), []byte("b")}
	ensureProcess(t, stg, pid)

	batch := &AggregatorBallotBatch{
		ProcessID: types.HexBytes(pid),
		Ballots: []*AggregatorBallot{
			mkAggBallot(ids[0]),
			mkAggBallot(ids[1]),
		},
	}
	c.Assert(stg.PushBallotBatch(batch), qt.IsNil)

	// fetch batch to get key (creates reservation)
	_, key, err := stg.NextBallotBatch(pid)
	c.Assert(err, qt.IsNil)

	// mark failed
	err = stg.MarkBallotBatchFailed(key)
	c.Assert(err, qt.IsNil)

	// should be no more batches
	_, _, err = stg.NextBallotBatch(pid)
	c.Assert(errors.Is(err, ErrNoMoreElements), qt.IsTrue)

	// voteIDs should now be error
	for _, id := range ids {
		status, err := stg.VoteIDStatus(pid, id)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, VoteIDStatusError)
	}
}

func TestBallotQueue_RemoveStateTransitionBatchesByProcess(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid1 := []byte("p1")
	pid2 := []byte("p2")
	ensureProcess(t, stg, pid1)
	ensureProcess(t, stg, pid2)

	// push state transition batches
	stb1 := &StateTransitionBatch{
		ProcessID: types.HexBytes(pid1),
		Ballots: []*AggregatorBallot{
			mkAggBallot([]byte("a1")),
		},
	}
	stb2 := &StateTransitionBatch{
		ProcessID: types.HexBytes(pid2),
		Ballots: []*AggregatorBallot{
			mkAggBallot([]byte("b1")),
		},
	}

	c.Assert(stg.PushStateTransitionBatch(stb1), qt.IsNil)
	c.Assert(stg.PushStateTransitionBatch(stb2), qt.IsNil)

	// remove for pid1
	err := stg.RemoveStateTransitionBatchesByProcess(pid1)
	c.Assert(err, qt.IsNil)

	// none left for pid1
	_, _, err = stg.NextStateTransitionBatch(pid1)
	c.Assert(errors.Is(err, ErrNoMoreElements), qt.IsTrue)

	// pid2 still present
	_, _, err = stg.NextStateTransitionBatch(pid2)
	c.Assert(err, qt.IsNil)
}

func TestBallotQueue_MarkStateTransitionBatchDone(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := []byte("p1")
	ids := [][]byte{[]byte("a"), []byte("b")}
	ensureProcess(t, stg, pid)

	// push a state transition batch (also sets statuses to processed)
	stb := &StateTransitionBatch{
		ProcessID: types.HexBytes(pid),
		Ballots: []*AggregatorBallot{
			mkAggBallot(ids[0]),
			mkAggBallot(ids[1]),
		},
	}
	c.Assert(stg.PushStateTransitionBatch(stb), qt.IsNil)

	// fetch to create reservation and get key
	_, key, err := stg.NextStateTransitionBatch(pid)
	c.Assert(err, qt.IsNil)

	// mark done
	err = stg.MarkStateTransitionBatchDone(key, pid)
	c.Assert(err, qt.IsNil)

	// should be none left
	_, _, err = stg.NextStateTransitionBatch(pid)
	c.Assert(errors.Is(err, ErrNoMoreElements), qt.IsTrue)

	// voteIDs should now be settled
	for _, id := range ids {
		status, err := stg.VoteIDStatus(pid, id)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, VoteIDStatusSettled)
	}
}

// TestMarkStateTransitionOutdated tests the MarkStateTransitionOutdated functionality
func TestMarkStateTransitionOutdated(t *testing.T) {
	c := qt.New(t)

	// Create a test storage instance
	dbDir := t.TempDir()
	testdb, err := metadb.New(db.TypePebble, dbDir)
	c.Assert(err, qt.IsNil)

	// Create storage instance
	stg := New(testdb)
	defer stg.Close()

	// Test process ID
	processID := []byte("test-process-outdated")

	// Create test ballots
	ballots := []*AggregatorBallot{
		{
			VoteID:  []byte("vote1"),
			Address: big.NewInt(1001),
		},
		{
			VoteID:  []byte("vote2"),
			Address: big.NewInt(1002),
		},
		{
			VoteID:  []byte("vote3"),
			Address: big.NewInt(1003),
		},
	}

	// Set vote IDs to processed status (simulating they were processed)
	for _, ballot := range ballots {
		err := stg.setVoteIDStatus(processID, ballot.VoteID, VoteIDStatusProcessed)
		c.Assert(err, qt.IsNil)
	}

	// Create and manually push a state transition batch (bypassing process stats)
	stb := &StateTransitionBatch{
		ProcessID: processID,
		Ballots:   ballots,
		Inputs: StateTransitionBatchProofInputs{
			RootHashBefore: big.NewInt(12345),
			RootHashAfter:  big.NewInt(67890),
			NumNewVotes:    3,
			NumOverwritten: 0,
		},
	}

	// Manually encode and store the state transition batch to avoid process stats updates
	val, err := EncodeArtifact(stb)
	c.Assert(err, qt.IsNil)
	key := hashKey(val)
	fullKey := append([]byte{}, processID...)
	fullKey = append(fullKey, key...)
	err = stg.setArtifact(stateTransitionPrefix, fullKey, stb)
	c.Assert(err, qt.IsNil)

	// Get the next state transition batch to verify it exists
	retrievedBatch, retrievedKey, err := stg.NextStateTransitionBatch(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(retrievedBatch, qt.Not(qt.IsNil))
	c.Assert(len(retrievedBatch.Ballots), qt.Equals, 3)

	// Verify vote IDs are still in processed status before marking outdated
	for _, ballot := range ballots {
		status, err := stg.VoteIDStatus(processID, ballot.VoteID)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, VoteIDStatusProcessed)
	}

	// Mark the state transition batch as outdated
	err = stg.MarkStateTransitionBatchOutdated(retrievedKey)
	c.Assert(err, qt.IsNil)

	// Verify that the batch is no longer available
	_, _, err = stg.NextStateTransitionBatch(processID)
	c.Assert(err, qt.Equals, ErrNoMoreElements)

	// Verify that vote IDs are still in processed status (not changed to error)
	for _, ballot := range ballots {
		status, err := stg.VoteIDStatus(processID, ballot.VoteID)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, VoteIDStatusProcessed, qt.Commentf("vote ID status should remain processed after marking outdated"))
	}

	// Test marking outdated with non-existent key
	nonExistentKey := []byte("non-existent-key")
	err = stg.MarkStateTransitionBatchOutdated(nonExistentKey)
	c.Assert(err, qt.IsNil, qt.Commentf("marking non-existent batch as outdated should not error"))
}

// TestMarkStateTransitionOutdatedVsMarkDone tests the difference between outdated and done
func TestMarkStateTransitionOutdatedVsMarkDone(t *testing.T) {
	c := qt.New(t)

	// Create a test storage instance
	dbDir := t.TempDir()
	testdb, err := metadb.New(db.TypePebble, dbDir)
	c.Assert(err, qt.IsNil)

	// Create storage instance
	stg := New(testdb)
	defer stg.Close()

	// Test process ID
	processID := []byte("test-process-comparison")

	// Create test ballots
	ballots := []*AggregatorBallot{
		{
			VoteID:  []byte("vote1"),
			Address: big.NewInt(2001),
		},
		{
			VoteID:  []byte("vote2"),
			Address: big.NewInt(2002),
		},
	}

	// Set vote IDs to processed status
	for _, ballot := range ballots {
		err := stg.setVoteIDStatus(processID, ballot.VoteID, VoteIDStatusProcessed)
		c.Assert(err, qt.IsNil)
	}

	// Create and manually store two state transition batches (bypassing process stats)
	stb1 := &StateTransitionBatch{
		ProcessID: processID,
		Ballots:   ballots,
		Inputs: StateTransitionBatchProofInputs{
			RootHashBefore: big.NewInt(11111),
			RootHashAfter:  big.NewInt(22222),
			NumNewVotes:    2,
			NumOverwritten: 0,
		},
	}

	stb2 := &StateTransitionBatch{
		ProcessID: processID,
		Ballots:   ballots,
		Inputs: StateTransitionBatchProofInputs{
			RootHashBefore: big.NewInt(33333),
			RootHashAfter:  big.NewInt(44444),
			NumNewVotes:    2,
			NumOverwritten: 0,
		},
	}

	// Manually encode and store the first state transition batch
	val1, err := EncodeArtifact(stb1)
	c.Assert(err, qt.IsNil)
	key1 := hashKey(val1)
	fullKey1 := append([]byte{}, processID...)
	fullKey1 = append(fullKey1, key1...)
	err = stg.setArtifact(stateTransitionPrefix, fullKey1, stb1)
	c.Assert(err, qt.IsNil)

	// Manually encode and store the second state transition batch
	val2, err := EncodeArtifact(stb2)
	c.Assert(err, qt.IsNil)
	key2 := hashKey(val2)
	fullKey2 := append([]byte{}, processID...)
	fullKey2 = append(fullKey2, key2...)
	err = stg.setArtifact(stateTransitionPrefix, fullKey2, stb2)
	c.Assert(err, qt.IsNil)

	// Get the first batch and mark it as outdated
	batch1, retrievedKey1, err := stg.NextStateTransitionBatch(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(batch1, qt.Not(qt.IsNil))

	err = stg.MarkStateTransitionBatchOutdated(retrievedKey1)
	c.Assert(err, qt.IsNil)

	// Get the second batch and mark it as done
	batch2, retrievedKey2, err := stg.NextStateTransitionBatch(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(batch2, qt.Not(qt.IsNil))

	err = stg.MarkStateTransitionBatchDone(retrievedKey2, processID)
	c.Assert(err, qt.IsNil)

	// Verify vote IDs status after both operations
	for _, ballot := range ballots {
		status, err := stg.VoteIDStatus(processID, ballot.VoteID)
		c.Assert(err, qt.IsNil)
		// After MarkStateTransitionBatchDone, vote IDs should be settled
		c.Assert(status, qt.Equals, VoteIDStatusSettled, qt.Commentf("vote ID should be settled after MarkStateTransitionBatchDone"))
	}

	// Verify no more batches are available
	_, _, err = stg.NextStateTransitionBatch(processID)
	c.Assert(err, qt.Equals, ErrNoMoreElements)
}

// TestMarkStateTransitionOutdatedWithCorruptedData tests handling of corrupted batch data
func TestMarkStateTransitionOutdatedWithCorruptedData(t *testing.T) {
	c := qt.New(t)

	// Create a test storage instance
	dbDir := t.TempDir()
	testdb, err := metadb.New(db.TypePebble, dbDir)
	c.Assert(err, qt.IsNil)

	// Create storage instance
	stg := New(testdb)
	defer stg.Close()

	// Manually insert corrupted data into the state transition prefix
	corruptedKey := []byte("corrupted-key")
	corruptedData := []byte("this-is-not-valid-cbor-data")

	// Use internal method to set corrupted data
	err = stg.setArtifact(stateTransitionPrefix, corruptedKey, corruptedData, ArtifactEncodingJSON)
	c.Assert(err, qt.IsNil)

	// Create a reservation for the corrupted key
	err = stg.setReservation(stateTransitionReservPrefix, corruptedKey)
	c.Assert(err, qt.IsNil)

	// Try to mark the corrupted batch as outdated - should handle gracefully
	err = stg.MarkStateTransitionBatchOutdated(corruptedKey)
	c.Assert(err, qt.IsNil, qt.Commentf("should handle corrupted data gracefully"))

	// Verify the corrupted data and reservation are cleaned up
	var testBatch StateTransitionBatch
	err = stg.getArtifact(stateTransitionPrefix, corruptedKey, &testBatch)
	c.Assert(err, qt.Equals, ErrNotFound, qt.Commentf("corrupted batch should be removed"))

	// Verify reservation is cleaned up
	isReserved := stg.isReserved(stateTransitionReservPrefix, corruptedKey)
	c.Assert(isReserved, qt.IsFalse, qt.Commentf("reservation should be removed"))
}
