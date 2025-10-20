package storage

import (
	"errors"
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/types"
)

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

	c.Assert(stg.PushAggregatorBatch(batch1), qt.IsNil)
	c.Assert(stg.PushAggregatorBatch(batch2), qt.IsNil)

	// remove batches for pid1
	err := stg.RemoveAggregatorBatchesByProcess(pid1)
	c.Assert(err, qt.IsNil)

	// no batch for pid1
	_, _, err = stg.NextAggregatorBatch(pid1)
	c.Assert(errors.Is(err, ErrNoMoreElements), qt.IsTrue)

	// but pid2 still available
	_, _, err = stg.NextAggregatorBatch(pid2)
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
	c.Assert(stg.PushAggregatorBatch(batch), qt.IsNil)

	// fetch batch to get key (creates reservation)
	_, key, err := stg.NextAggregatorBatch(pid)
	c.Assert(err, qt.IsNil)

	// mark failed
	err = stg.MarkAggregatorBatchFailed(key)
	c.Assert(err, qt.IsNil)

	// should be no more batches
	_, _, err = stg.NextAggregatorBatch(pid)
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
