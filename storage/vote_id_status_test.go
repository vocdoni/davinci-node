package storage

import (
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/metadb"
)

func TestVoteIDStatus(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create test process ID
	processID := &types.ProcessID{
		Address: common.Address{},
		Nonce:   42,
		ChainID: 1,
	}
	pidBytes := processID.Marshal()

	// Create some test vote IDs
	voteID1 := []byte("vote1")
	voteID2 := []byte("vote2")
	voteID3 := []byte("vote3")

	// Test 1: Set and get statuses
	err = st.setVoteIDStatus(pidBytes, voteID1, VoteIDStatusPending)
	c.Assert(err, qt.IsNil)

	status, err := st.VoteIDStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusPending)

	// Test status name
	c.Assert(VoteIDStatusName(status), qt.Equals, "pending")

	// Test 2: Update status
	err = st.setVoteIDStatus(pidBytes, voteID1, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)

	status, err = st.VoteIDStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusVerified)

	// Test 3: Non-existent status
	_, err = st.VoteIDStatus(pidBytes, []byte("nonexistent"))
	c.Assert(err, qt.Equals, ErrNotFound)

	// Test 4: MarkVoteIDsSettled
	// Setup multiple vote IDs
	err = st.setVoteIDStatus(pidBytes, voteID1, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)
	err = st.setVoteIDStatus(pidBytes, voteID2, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)
	err = st.setVoteIDStatus(pidBytes, voteID3, VoteIDStatusProcessed)
	c.Assert(err, qt.IsNil)

	// Mark vote IDs as settled
	err = st.MarkVoteIDsSettled(pidBytes, [][]byte{voteID1, voteID2})
	c.Assert(err, qt.IsNil)

	// Verify vote IDs were marked as settled
	status1, err := st.VoteIDStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status1, qt.Equals, VoteIDStatusSettled)

	status2, err := st.VoteIDStatus(pidBytes, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status2, qt.Equals, VoteIDStatusSettled)

	// voteID3 should still be in "processed" state
	status3, err := st.VoteIDStatus(pidBytes, voteID3)
	c.Assert(err, qt.IsNil)
	c.Assert(status3, qt.Equals, VoteIDStatusProcessed)

	// Test 5: CleanProcessVoteIDs
	// First verify all vote IDs have the right statuses before cleaning
	status1, err = st.VoteIDStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status1, qt.Equals, VoteIDStatusSettled)

	status2, err = st.VoteIDStatus(pidBytes, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status2, qt.Equals, VoteIDStatusSettled)

	status3, err = st.VoteIDStatus(pidBytes, voteID3)
	c.Assert(err, qt.IsNil)
	c.Assert(status3, qt.Equals, VoteIDStatusProcessed)

	// Now clean all vote ID statuses for this process
	count, err := st.CleanProcessVoteIDs(pidBytes)
	c.Assert(err, qt.IsNil)
	c.Assert(count >= 3, qt.IsTrue, qt.Commentf("expected at least 3 entries to clean, got %d", count))

	// After cleaning, verify we can create new entries in place of the cleaned ones
	// This indirectly verifies the old entries were removed

	// First for voteID1
	err = st.setVoteIDStatus(pidBytes, voteID1, VoteIDStatusPending)
	c.Assert(err, qt.IsNil)

	// Check it was set correctly
	var status1Value int
	status1Value, err = st.VoteIDStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status1Value, qt.Equals, VoteIDStatusPending)

	// Also for voteID2
	err = st.setVoteIDStatus(pidBytes, voteID2, VoteIDStatusVerified)
	c.Assert(err, qt.IsNil)

	var status2Value int
	status2Value, err = st.VoteIDStatus(pidBytes, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status2Value, qt.Equals, VoteIDStatusVerified)

	// Test 6: Clean and verify the cleanup
	// Clean again and we should get the number of entries we just added
	count2, err := st.CleanProcessVoteIDs(pidBytes)
	c.Assert(err, qt.IsNil)
	c.Assert(count2, qt.Equals, 2, qt.Commentf("expected 2 entries to be cleaned, got %d", count2))
}
