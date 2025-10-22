package storage

import (
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/types"
)

func TestVoteIDStatus(t *testing.T) {
	c := qt.New(t)

	// Create storage instance
	db := metadb.NewTest(t)
	st := New(db)
	defer st.Close()

	// Create test process
	pid := &types.ProcessID{
		Address: [20]byte{1},
		Nonce:   1,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}
	pidBytes := pid.Marshal()

	voteID1 := []byte("vote1")
	voteID2 := []byte("vote2")
	voteID3 := []byte("vote3")

	// Test 1: Set and get statuses
	err := st.setVoteIDStatus(pidBytes, voteID1, VoteIDStatusPending)
	c.Assert(err, qt.IsNil)

	status, err := st.VoteIDStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusPending)

	// Test 2: Update status
	err = st.setVoteIDStatus(pidBytes, voteID1, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusVerified)

	// Test 3: Mark as settled
	// Setup multiple vote IDs
	err = st.setVoteIDStatus(pidBytes, voteID1, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)

	err = st.setVoteIDStatus(pidBytes, voteID2, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)

	err = st.setVoteIDStatus(pidBytes, voteID3, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)

	// Mark them as settled
	err = st.MarkVoteIDsSettled(pidBytes, [][]byte{voteID1, voteID2, voteID3})
	c.Assert(err, qt.IsNil)

	// Verify all are settled
	for _, voteID := range [][]byte{voteID1, voteID2, voteID3} {
		status, err := st.VoteIDStatus(pidBytes, voteID)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, VoteIDStatusSettled)
	}

	// Test 4: Non-existent vote ID
	_, err = st.VoteIDStatus(pidBytes, []byte("nonexistent"))
	c.Assert(err, qt.Equals, ErrNotFound)
}

func TestVoteIDStatusTransitionProtection(t *testing.T) {
	c := qt.New(t)

	// Create storage instance
	db := metadb.NewTest(t)
	st := New(db)
	defer st.Close()

	pid := &types.ProcessID{
		Address: [20]byte{1},
		Nonce:   1,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}
	pidBytes := pid.Marshal()

	// Test 1: SETTLED status cannot be changed
	voteID1 := []byte("vote1")
	err := st.setVoteIDStatus(pidBytes, voteID1, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)

	err = st.MarkVoteIDsSettled(pidBytes, [][]byte{voteID1})
	c.Assert(err, qt.IsNil)

	status, err := st.VoteIDStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusSettled)

	// Try to change to ERROR - should be silently ignored
	err = st.setVoteIDStatus(pidBytes, voteID1, VoteIDStatusError)
	c.Assert(err, qt.IsNil) // No error, but status should remain SETTLED

	status, err = st.VoteIDStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusSettled, qt.Commentf("SETTLED status should not change"))

	// Try to change to PENDING - should be silently ignored
	err = st.setVoteIDStatus(pidBytes, voteID1, VoteIDStatusPending)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusSettled, qt.Commentf("SETTLED status should not change"))

	// Test 2: Valid forward progression
	voteID2 := []byte("vote2")
	err = st.setVoteIDStatus(pidBytes, voteID2, VoteIDStatusPending)
	c.Assert(err, qt.IsNil)

	err = st.setVoteIDStatus(pidBytes, voteID2, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pidBytes, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusVerified)

	err = st.setVoteIDStatus(pidBytes, voteID2, VoteIDStatusAggregated)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pidBytes, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusAggregated)

	err = st.setVoteIDStatus(pidBytes, voteID2, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pidBytes, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusProcessed)

	err = st.setVoteIDStatus(pidBytes, voteID2, VoteIDStatusSettled)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pidBytes, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusSettled)

	// Test 3: ERROR can be set from any status (except SETTLED)
	voteID3 := []byte("vote3")
	err = st.setVoteIDStatus(pidBytes, voteID3, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)

	err = st.setVoteIDStatus(pidBytes, voteID3, VoteIDStatusError)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pidBytes, voteID3)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusError)

	// Test 4: TIMEOUT can be set from any status (except SETTLED)
	voteID4 := []byte("vote4")
	err = st.setVoteIDStatus(pidBytes, voteID4, VoteIDStatusAggregated)
	c.Assert(err, qt.IsNil)

	err = st.setVoteIDStatus(pidBytes, voteID4, VoteIDStatusTimeout)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pidBytes, voteID4)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusTimeout)

	// Test 5: Invalid backward transition (logs warning but allows it)
	voteID5 := []byte("vote5")
	err = st.setVoteIDStatus(pidBytes, voteID5, VoteIDStatusAggregated)
	c.Assert(err, qt.IsNil)

	// Try to go back to VERIFIED - should log warning but allow
	err = st.setVoteIDStatus(pidBytes, voteID5, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pidBytes, voteID5)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusVerified, qt.Commentf("backward transition allowed with warning"))

	// Test 6: SETTLED can only be reached from PROCESSED
	voteID6 := []byte("vote6")
	err = st.setVoteIDStatus(pidBytes, voteID6, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)

	// Try to jump directly to SETTLED - should log warning but allow
	err = st.setVoteIDStatus(pidBytes, voteID6, VoteIDStatusSettled)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pidBytes, voteID6)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusSettled, qt.Commentf("invalid jump to SETTLED allowed with warning"))
}

func TestMarkProcessVoteIDsTimeout(t *testing.T) {
	c := qt.New(t)

	// Create storage instance
	db := metadb.NewTest(t)
	st := New(db)
	defer st.Close()

	pid := &types.ProcessID{
		Address: [20]byte{1},
		Nonce:   1,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}
	pidBytes := pid.Marshal()

	// Create vote IDs with different statuses
	voteID1 := []byte("vote1")
	voteID2 := []byte("vote2")
	voteID3 := []byte("vote3")
	voteID4 := []byte("vote4")

	err := st.setVoteIDStatus(pidBytes, voteID1, VoteIDStatusPending)
	c.Assert(err, qt.IsNil)

	err = st.setVoteIDStatus(pidBytes, voteID2, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)

	err = st.setVoteIDStatus(pidBytes, voteID3, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)

	// Mark voteID4 as settled
	err = st.setVoteIDStatus(pidBytes, voteID4, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)
	err = st.MarkVoteIDsSettled(pidBytes, [][]byte{voteID4})
	c.Assert(err, qt.IsNil)

	// Mark all unsettled votes as timeout
	count, err := st.MarkProcessVoteIDsTimeout(pidBytes)
	c.Assert(err, qt.IsNil)
	c.Assert(count, qt.Equals, 3, qt.Commentf("should mark 3 votes as timeout (not the settled one)"))

	// Verify statuses
	status, err := st.VoteIDStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusTimeout)

	status, err = st.VoteIDStatus(pidBytes, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusTimeout)

	status, err = st.VoteIDStatus(pidBytes, voteID3)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusTimeout)

	// Settled vote should remain settled
	status, err = st.VoteIDStatus(pidBytes, voteID4)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusSettled, qt.Commentf("settled vote should not be marked as timeout"))
}

func TestVoteIDStatusName(t *testing.T) {
	c := qt.New(t)

	c.Assert(VoteIDStatusName(VoteIDStatusPending), qt.Equals, "pending")
	c.Assert(VoteIDStatusName(VoteIDStatusVerified), qt.Equals, "verified")
	c.Assert(VoteIDStatusName(VoteIDStatusAggregated), qt.Equals, "aggregated")
	c.Assert(VoteIDStatusName(VoteIDStatusProcessed), qt.Equals, "processed")
	c.Assert(VoteIDStatusName(VoteIDStatusSettled), qt.Equals, "settled")
	c.Assert(VoteIDStatusName(VoteIDStatusError), qt.Equals, "error")
	c.Assert(VoteIDStatusName(VoteIDStatusTimeout), qt.Equals, "timeout")
	c.Assert(VoteIDStatusName(999), qt.Equals, "unknown_status_999")
}
