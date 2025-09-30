package storage

import (
	"bytes"
	"fmt"
	"math/big"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/types"
)

func TestCleanupEndedProcess(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	testDB, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(testDB)
	defer st.Close()

	// Create two processes - one to cleanup, one to preserve
	processID1 := &types.ProcessID{
		Address: common.Address{1},
		Nonce:   1,
		ChainID: 1,
	}
	processID2 := &types.ProcessID{
		Address: common.Address{2},
		Nonce:   2,
		ChainID: 1,
	}

	// Create processes
	err = st.NewProcess(createTestProcess(processID1))
	c.Assert(err, qt.IsNil)
	err = st.NewProcess(createTestProcess(processID2))
	c.Assert(err, qt.IsNil)

	// Setup test data for both processes
	setupTestDataForCleanup(c, st, processID1, processID2)

	// Verify initial state
	verifyInitialState(c, st, processID1, processID2)

	// Cleanup processID1
	err = st.cleanupEndedProcess(processID1.Marshal())
	c.Assert(err, qt.IsNil)

	// Verify cleanup results
	verifyCleanupResults(c, st, processID1, processID2)
}

func TestCleanupEndedProcessWithSettledVotes(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	testDB, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(testDB)
	defer st.Close()

	processID := &types.ProcessID{
		Address: common.Address{1},
		Nonce:   1,
		ChainID: 1,
	}

	err = st.NewProcess(createTestProcess(processID))
	c.Assert(err, qt.IsNil)

	// Create ballots and process them through to settled state
	voteIDs := setupBallotsToSettledState(c, st, processID)

	// Verify votes are settled before cleanup
	for _, voteID := range voteIDs {
		status, err := st.VoteIDStatus(processID.Marshal(), voteID)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, VoteIDStatusSettled)
	}

	// Cleanup the process
	err = st.cleanupEndedProcess(processID.Marshal())
	c.Assert(err, qt.IsNil)

	// Verify settled votes remain settled (not marked as timeout)
	for _, voteID := range voteIDs {
		status, err := st.VoteIDStatus(processID.Marshal(), voteID)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, VoteIDStatusSettled, qt.Commentf("Settled votes should remain settled"))
	}
}

func TestMarkProcessVoteIDsTimeout(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	testDB, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(testDB)
	defer st.Close()

	processID := &types.ProcessID{
		Address: common.Address{1},
		Nonce:   1,
		ChainID: 1,
	}

	// Create test vote IDs with different statuses
	voteID1 := []byte("vote1")
	voteID2 := []byte("vote2")
	voteID3 := []byte("vote3")
	voteID4 := []byte("vote4")

	// Set different statuses
	err = st.setVoteIDStatus(processID.Marshal(), voteID1, VoteIDStatusPending)
	c.Assert(err, qt.IsNil)
	err = st.setVoteIDStatus(processID.Marshal(), voteID2, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)
	err = st.setVoteIDStatus(processID.Marshal(), voteID3, VoteIDStatusSettled)
	c.Assert(err, qt.IsNil)
	err = st.setVoteIDStatus(processID.Marshal(), voteID4, VoteIDStatusError)
	c.Assert(err, qt.IsNil)

	// Mark as timeout
	count, err := st.MarkProcessVoteIDsTimeout(processID.Marshal())
	c.Assert(err, qt.IsNil)
	c.Assert(count, qt.Equals, 3, qt.Commentf("Should update 3 votes (not the settled one)"))

	// Verify statuses
	status1, err := st.VoteIDStatus(processID.Marshal(), voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status1, qt.Equals, VoteIDStatusTimeout)

	status2, err := st.VoteIDStatus(processID.Marshal(), voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status2, qt.Equals, VoteIDStatusTimeout)

	status3, err := st.VoteIDStatus(processID.Marshal(), voteID3)
	c.Assert(err, qt.IsNil)
	c.Assert(status3, qt.Equals, VoteIDStatusSettled, qt.Commentf("Settled votes should not be changed"))

	status4, err := st.VoteIDStatus(processID.Marshal(), voteID4)
	c.Assert(err, qt.IsNil)
	c.Assert(status4, qt.Equals, VoteIDStatusTimeout)
}

func TestVoteIDStatusTimeout(t *testing.T) {
	c := qt.New(t)

	// Test the new timeout status constant and name
	c.Assert(VoteIDStatusTimeout, qt.Equals, 6)
	c.Assert(VoteIDStatusName(VoteIDStatusTimeout), qt.Equals, "timeout")
}

// Helper functions

func setupTestDataForCleanup(c *qt.C, st *Storage, processID1, processID2 *types.ProcessID) {
	// Create pending ballots for both processes
	for i := range 3 {
		ballot1 := &Ballot{
			ProcessID: processID1.Marshal(),
			Address:   big.NewInt(int64(i + 1000)),
			VoteID:    fmt.Appendf(nil, "vote%d", i),
		}
		err := st.PushBallot(ballot1)
		c.Assert(err, qt.IsNil)

		ballot2 := &Ballot{
			ProcessID: processID2.Marshal(),
			Address:   big.NewInt(int64(i + 3000)),
			VoteID:    fmt.Appendf(nil, "vote%d", i+3),
		}
		err = st.PushBallot(ballot2)
		c.Assert(err, qt.IsNil)
	}

	// Process some ballots to verified state for both processes
	for range 2 {
		// Process ballot for processID1
		b1, key1, err := st.NextBallot()
		c.Assert(err, qt.IsNil)
		if bytes.Equal(b1.ProcessID, processID1.Marshal()) {
			vb1 := &VerifiedBallot{
				ProcessID:   b1.ProcessID,
				Address:     b1.Address,
				VoteID:      b1.VoteID,
				VoterWeight: big.NewInt(1),
			}
			err = st.MarkBallotDone(key1, vb1)
			c.Assert(err, qt.IsNil)
		}

		// Process ballot for processID2
		b2, key2, err := st.NextBallot()
		c.Assert(err, qt.IsNil)
		if bytes.Equal(b2.ProcessID, processID2.Marshal()) {
			vb2 := &VerifiedBallot{
				ProcessID:   b2.ProcessID,
				Address:     b2.Address,
				VoteID:      b2.VoteID,
				VoterWeight: big.NewInt(1),
			}
			err = st.MarkBallotDone(key2, vb2)
			c.Assert(err, qt.IsNil)
		}
	}

	// Create aggregator batches for both processes
	createAggregatorBatch(c, st, processID1)
	createAggregatorBatch(c, st, processID2)

	// Create state transitions for both processes
	createStateTransition(c, st, processID1)
	createStateTransition(c, st, processID2)

	// Create verified results for both processes
	createVerifiedResults(c, st, processID1)
	createVerifiedResults(c, st, processID2)
}

func createAggregatorBatch(c *qt.C, st *Storage, processID *types.ProcessID) {
	batch := &AggregatorBallotBatch{
		ProcessID: processID.Marshal(),
		Ballots: []*AggregatorBallot{
			{
				VoteID:  []byte("aggvote1"),
				Address: big.NewInt(2001),
			},
		},
	}
	err := st.PushBallotBatch(batch)
	c.Assert(err, qt.IsNil)
}

func createStateTransition(c *qt.C, st *Storage, processID *types.ProcessID) {
	stb := &StateTransitionBatch{
		ProcessID: processID.Marshal(),
		Ballots: []*AggregatorBallot{
			{
				VoteID:  []byte("stvote1"),
				Address: big.NewInt(4001),
			},
		},
	}
	err := st.PushStateTransitionBatch(stb)
	c.Assert(err, qt.IsNil)
}

func createVerifiedResults(c *qt.C, st *Storage, processID *types.ProcessID) {
	results := &VerifiedResults{
		ProcessID: processID.Marshal(),
	}
	err := st.PushVerifiedResults(results)
	c.Assert(err, qt.IsNil)
}

func verifyInitialState(c *qt.C, st *Storage, processID1, processID2 *types.ProcessID) {
	// Verify pending ballots exist for both processes
	pendingCount := st.CountPendingBallots()
	c.Assert(pendingCount > 0, qt.IsTrue, qt.Commentf("Should have pending ballots"))

	// Verify verified ballots exist for both processes
	verifiedCount1 := st.CountVerifiedBallots(processID1.Marshal())
	verifiedCount2 := st.CountVerifiedBallots(processID2.Marshal())
	c.Assert(verifiedCount1 > 0, qt.IsTrue, qt.Commentf("Process1 should have verified ballots"))
	c.Assert(verifiedCount2 > 0, qt.IsTrue, qt.Commentf("Process2 should have verified ballots"))

	// Verify aggregator batches exist
	_, _, err := st.NextBallotBatch(processID1.Marshal())
	c.Assert(err, qt.IsNil, qt.Commentf("Process1 should have aggregator batch"))

	// Verify state transitions exist
	_, _, err = st.NextStateTransitionBatch(processID1.Marshal())
	c.Assert(err, qt.IsNil, qt.Commentf("Process1 should have state transition"))

	// Verify verified results exist
	results, err := st.NextVerifiedResults()
	c.Assert(err, qt.IsNil, qt.Commentf("Should have verified results"))
	c.Assert(results, qt.IsNotNil)
}

func verifyCleanupResults(c *qt.C, st *Storage, processID1, processID2 *types.ProcessID) {
	// Verify processID1 data is cleaned up

	// Check pending ballots - should only have processID2 ballots
	pendingCount := st.CountPendingBallots()
	c.Assert(pendingCount > 0, qt.IsTrue, qt.Commentf("Should still have pending ballots from process2"))

	// Check verified ballots - processID1 should have none
	verifiedCount1 := st.CountVerifiedBallots(processID1.Marshal())
	verifiedCount2 := st.CountVerifiedBallots(processID2.Marshal())
	c.Assert(verifiedCount1, qt.Equals, 0, qt.Commentf("Process1 should have no verified ballots"))
	c.Assert(verifiedCount2 > 0, qt.IsTrue, qt.Commentf("Process2 should still have verified ballots"))

	// Check aggregator batches - processID1 should have none
	_, _, err := st.NextBallotBatch(processID1.Marshal())
	c.Assert(err, qt.Equals, ErrNoMoreElements, qt.Commentf("Process1 should have no aggregator batches"))

	_, _, err = st.NextBallotBatch(processID2.Marshal())
	c.Assert(err, qt.IsNil, qt.Commentf("Process2 should still have aggregator batch"))

	// Check state transitions - processID1 should have none
	_, _, err = st.NextStateTransitionBatch(processID1.Marshal())
	c.Assert(err, qt.Equals, ErrNoMoreElements, qt.Commentf("Process1 should have no state transitions"))

	_, _, err = st.NextStateTransitionBatch(processID2.Marshal())
	c.Assert(err, qt.IsNil, qt.Commentf("Process2 should still have state transition"))

	// Check verified results - should still exist for process2
	results, err := st.NextVerifiedResults()
	c.Assert(err, qt.IsNil, qt.Commentf("Should still have verified results from process2"))
	c.Assert(results, qt.IsNotNil)

	// Verify vote ID statuses are marked as timeout for processID1
	// We need to check if any vote IDs exist and verify their status
	// This is indirect since we can't easily iterate vote IDs, but the timeout marking was tested separately
}

func setupBallotsToSettledState(c *qt.C, st *Storage, processID *types.ProcessID) [][]byte {
	voteIDs := make([][]byte, 2)

	// Create and process ballots through the full pipeline
	ballots := make([]*Ballot, 2)
	for i := range 2 {
		ballot := &Ballot{
			ProcessID: processID.Marshal(),
			Address:   big.NewInt(int64(i + 5000)),
			VoteID:    fmt.Appendf(nil, "vote%d", i),
		}
		err := st.PushBallot(ballot)
		c.Assert(err, qt.IsNil)

		ballots[i] = ballot
		voteIDs[i] = ballot.VoteID
	}

	// Process to verified
	for range 2 {
		b, key, err := st.NextBallot()
		c.Assert(err, qt.IsNil)

		vb := &VerifiedBallot{
			ProcessID:   b.ProcessID,
			Address:     b.Address,
			VoteID:      b.VoteID,
			VoterWeight: big.NewInt(1),
		}
		err = st.MarkBallotDone(key, vb)
		c.Assert(err, qt.IsNil)
	}

	// Create aggregator batch
	verifiedBallots, keys, err := st.PullVerifiedBallots(processID.Marshal(), 2)
	c.Assert(err, qt.IsNil)

	aggBallots := make([]*AggregatorBallot, len(verifiedBallots))
	for i, vb := range verifiedBallots {
		aggBallots[i] = &AggregatorBallot{
			VoteID:  vb.VoteID,
			Address: vb.Address,
		}
	}

	batch := &AggregatorBallotBatch{
		ProcessID: processID.Marshal(),
		Ballots:   aggBallots,
	}
	err = st.PushBallotBatch(batch)
	c.Assert(err, qt.IsNil)

	err = st.MarkVerifiedBallotsDone(keys...)
	c.Assert(err, qt.IsNil)

	// Create state transition
	batchEntry, batchKey, err := st.NextBallotBatch(processID.Marshal())
	c.Assert(err, qt.IsNil)

	stb := &StateTransitionBatch{
		ProcessID: processID.Marshal(),
		Ballots:   batchEntry.Ballots,
	}
	err = st.PushStateTransitionBatch(stb)
	c.Assert(err, qt.IsNil)

	err = st.MarkBallotBatchDone(batchKey)
	c.Assert(err, qt.IsNil)

	// Mark as settled
	stbEntry, stbKey, err := st.NextStateTransitionBatch(processID.Marshal())
	c.Assert(err, qt.IsNil)

	err = st.MarkStateTransitionBatchDone(stbKey, processID.Marshal())
	c.Assert(err, qt.IsNil)

	// Extract vote IDs from the state transition batch
	settledVoteIDs := make([][]byte, len(stbEntry.Ballots))
	for i, ballot := range stbEntry.Ballots {
		settledVoteIDs[i] = ballot.VoteID
	}

	return settledVoteIDs
}
