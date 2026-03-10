package storage

import (
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/types"
)

func TestVoteIDStatus(t *testing.T) {
	c := qt.New(t)

	// Create storage instance
	db := metadb.NewTest(t)
	st := New(db)
	defer st.Close()

	// Create test process
	pid := testutil.DeterministicProcessID(1)
	process := testutil.RandomProcess(pid)
	c.Assert(st.NewProcess(process), qt.IsNil)

	voteID1 := testutil.RandomVoteID()
	voteID2 := testutil.RandomVoteID()
	voteID3 := testutil.RandomVoteID()

	// Test 1: Set and get statuses
	err := st.setVoteIDStatus(pid, voteID1, VoteIDStatusPending)
	c.Assert(err, qt.IsNil)

	status, err := st.VoteIDStatus(pid, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusPending)

	// Test 2: Update status
	err = st.setVoteIDStatus(pid, voteID1, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pid, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusVerified)

	// Test 3: Mark as done
	// Setup multiple vote IDs
	err = st.setVoteIDStatus(pid, voteID1, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)

	err = st.setVoteIDStatus(pid, voteID2, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)

	err = st.setVoteIDStatus(pid, voteID3, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)

	// Mark them as done
	err = st.MarkVoteIDsDone(pid, []types.VoteID{voteID1, voteID2, voteID3})
	c.Assert(err, qt.IsNil)

	// Verify all are done
	for _, voteID := range []types.VoteID{voteID1, voteID2, voteID3} {
		status, err := st.VoteIDStatus(pid, voteID)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, VoteIDStatusDone)
	}

	// Test 4: Non-existent vote ID
	_, err = st.VoteIDStatus(pid, testutil.RandomVoteID())
	c.Assert(err, qt.Equals, ErrNotFound)
}

func TestVoteIDStatusTransitionProtection(t *testing.T) {
	c := qt.New(t)

	// Create storage instance
	db := metadb.NewTest(t)
	st := New(db)
	defer st.Close()

	pid := testutil.DeterministicProcessID(1)
	process := testutil.RandomProcess(pid)
	c.Assert(st.NewProcess(process), qt.IsNil)

	// Test 1: DONE status cannot be changed
	voteID1 := testutil.RandomVoteID()
	err := st.setVoteIDStatus(pid, voteID1, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)

	err = st.MarkVoteIDsDone(pid, []types.VoteID{voteID1})
	c.Assert(err, qt.IsNil)

	status, err := st.VoteIDStatus(pid, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusDone)

	// Try to change to ERROR - should be silently ignored
	err = st.setVoteIDStatus(pid, voteID1, VoteIDStatusError)
	c.Assert(err, qt.IsNil) // No error, but status should remain DONE

	status, err = st.VoteIDStatus(pid, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusDone, qt.Commentf("DONE status should not change"))

	// Try to change to PENDING - should be silently ignored
	err = st.setVoteIDStatus(pid, voteID1, VoteIDStatusPending)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pid, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusDone, qt.Commentf("DONE status should not change"))

	// Test 2: Valid forward progression
	voteID2 := testutil.RandomVoteID()
	err = st.setVoteIDStatus(pid, voteID2, VoteIDStatusPending)
	c.Assert(err, qt.IsNil)

	err = st.setVoteIDStatus(pid, voteID2, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pid, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusVerified)

	err = st.setVoteIDStatus(pid, voteID2, VoteIDStatusAggregated)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pid, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusAggregated)

	err = st.setVoteIDStatus(pid, voteID2, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pid, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusProcessed)

	err = st.setVoteIDStatus(pid, voteID2, VoteIDStatusDone)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pid, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusDone)

	// Test 3: ERROR can be set from any status (except DONE)
	voteID3 := testutil.RandomVoteID()
	err = st.setVoteIDStatus(pid, voteID3, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)

	err = st.setVoteIDStatus(pid, voteID3, VoteIDStatusError)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pid, voteID3)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusError)

	// Test 4: TIMEOUT can be set from any status (except DONE)
	voteID4 := testutil.RandomVoteID()
	err = st.setVoteIDStatus(pid, voteID4, VoteIDStatusAggregated)
	c.Assert(err, qt.IsNil)

	err = st.setVoteIDStatus(pid, voteID4, VoteIDStatusTimeout)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pid, voteID4)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusTimeout)

	// Test 5: Invalid backward transition (logs warning but allows it)
	voteID5 := testutil.RandomVoteID()
	err = st.setVoteIDStatus(pid, voteID5, VoteIDStatusAggregated)
	c.Assert(err, qt.IsNil)

	// Try to go back to VERIFIED - should log warning but allow
	err = st.setVoteIDStatus(pid, voteID5, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pid, voteID5)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusVerified, qt.Commentf("backward transition allowed with warning"))

	// Test 6: DONE can only be reached from PROCESSED
	voteID6 := testutil.RandomVoteID()
	err = st.setVoteIDStatus(pid, voteID6, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)

	// Try to jump directly to DONE - should log warning but allow
	err = st.setVoteIDStatus(pid, voteID6, VoteIDStatusDone)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pid, voteID6)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusDone, qt.Commentf("invalid jump to DONE allowed with warning"))
}

func TestMarkProcessVoteIDsTimeout(t *testing.T) {
	c := qt.New(t)

	// Create storage instance
	db := metadb.NewTest(t)
	st := New(db)
	defer st.Close()

	pid := testutil.DeterministicProcessID(1)
	process := testutil.RandomProcess(pid)
	c.Assert(st.NewProcess(process), qt.IsNil)

	// Create vote IDs with different statuses
	voteID1 := testutil.RandomVoteID()
	voteID2 := testutil.RandomVoteID()
	voteID3 := testutil.RandomVoteID()
	voteID4 := testutil.RandomVoteID()

	err := st.setVoteIDStatus(pid, voteID1, VoteIDStatusPending)
	c.Assert(err, qt.IsNil)

	err = st.setVoteIDStatus(pid, voteID2, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)

	err = st.setVoteIDStatus(pid, voteID3, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)

	// Mark voteID4 as done
	err = st.setVoteIDStatus(pid, voteID4, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)
	err = st.MarkVoteIDsDone(pid, []types.VoteID{voteID4})
	c.Assert(err, qt.IsNil)

	// Mark all undone votes as timeout
	count, err := st.MarkProcessVoteIDsTimeout(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(count, qt.Equals, 3, qt.Commentf("should mark 3 votes as timeout (not the done one)"))

	// Verify statuses
	status, err := st.VoteIDStatus(pid, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusTimeout)

	status, err = st.VoteIDStatus(pid, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusTimeout)

	status, err = st.VoteIDStatus(pid, voteID3)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusTimeout)

	// Done vote should remain done
	status, err = st.VoteIDStatus(pid, voteID4)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusDone, qt.Commentf("done vote should not be marked as timeout"))
}

func TestVoteIDStatusName(t *testing.T) {
	c := qt.New(t)

	c.Assert(VoteIDStatusName(VoteIDStatusPending), qt.Equals, "pending")
	c.Assert(VoteIDStatusName(VoteIDStatusVerified), qt.Equals, "verified")
	c.Assert(VoteIDStatusName(VoteIDStatusAggregated), qt.Equals, "aggregated")
	c.Assert(VoteIDStatusName(VoteIDStatusProcessed), qt.Equals, "processed")
	c.Assert(VoteIDStatusName(VoteIDStatusDone), qt.Equals, "done")
	c.Assert(VoteIDStatusName(VoteIDStatusError), qt.Equals, "error")
	c.Assert(VoteIDStatusName(VoteIDStatusTimeout), qt.Equals, "timeout")
	c.Assert(VoteIDStatusName(999), qt.Equals, "unknown_status_999")
}
