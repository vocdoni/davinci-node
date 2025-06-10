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

func TestBallotStatus(t *testing.T) {
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
	err = st.setBallotStatus(pidBytes, voteID1, BallotStatusPending)
	c.Assert(err, qt.IsNil)

	status, err := st.BallotStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, BallotStatusPending)

	// Test status name
	c.Assert(BallotStatusName(status), qt.Equals, "pending")

	// Test 2: Update status
	err = st.setBallotStatus(pidBytes, voteID1, BallotStatusVerified)
	c.Assert(err, qt.IsNil)

	status, err = st.BallotStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, BallotStatusVerified)

	// Test 3: Non-existent status
	_, err = st.BallotStatus(pidBytes, []byte("nonexistent"))
	c.Assert(err, qt.Equals, ErrNotFound)

	// Test 4: MarkBallotsSettled
	// Setup multiple ballots
	err = st.setBallotStatus(pidBytes, voteID1, BallotStatusProcessed)
	c.Assert(err, qt.IsNil)
	err = st.setBallotStatus(pidBytes, voteID2, BallotStatusProcessed)
	c.Assert(err, qt.IsNil)
	err = st.setBallotStatus(pidBytes, voteID3, BallotStatusProcessed)
	c.Assert(err, qt.IsNil)

	// Mark ballots as settled
	err = st.MarkBallotsSettled(pidBytes, [][]byte{voteID1, voteID2})
	c.Assert(err, qt.IsNil)

	// Verify ballots were marked as settled
	status1, err := st.BallotStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status1, qt.Equals, BallotStatusSettled)

	status2, err := st.BallotStatus(pidBytes, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status2, qt.Equals, BallotStatusSettled)

	// voteID3 should still be in "processed" state
	status3, err := st.BallotStatus(pidBytes, voteID3)
	c.Assert(err, qt.IsNil)
	c.Assert(status3, qt.Equals, BallotStatusProcessed)

	// Test 5: CleanProcessBallots
	// First verify all ballots have the right statuses before cleaning
	status1, err = st.BallotStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status1, qt.Equals, BallotStatusSettled)

	status2, err = st.BallotStatus(pidBytes, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status2, qt.Equals, BallotStatusSettled)

	status3, err = st.BallotStatus(pidBytes, voteID3)
	c.Assert(err, qt.IsNil)
	c.Assert(status3, qt.Equals, BallotStatusProcessed)

	// Now clean all ballot statuses for this process
	count, err := st.CleanProcessBallots(pidBytes)
	c.Assert(err, qt.IsNil)
	c.Assert(count >= 3, qt.IsTrue, qt.Commentf("expected at least 3 entries to clean, got %d", count))

	// After cleaning, verify we can create new entries in place of the cleaned ones
	// This indirectly verifies the old entries were removed

	// First for voteID1
	err = st.setBallotStatus(pidBytes, voteID1, BallotStatusPending)
	c.Assert(err, qt.IsNil)

	// Check it was set correctly
	var status1Value int
	status1Value, err = st.BallotStatus(pidBytes, voteID1)
	c.Assert(err, qt.IsNil)
	c.Assert(status1Value, qt.Equals, BallotStatusPending)

	// Also for voteID2
	err = st.setBallotStatus(pidBytes, voteID2, BallotStatusVerified)
	c.Assert(err, qt.IsNil)

	var status2Value int
	status2Value, err = st.BallotStatus(pidBytes, voteID2)
	c.Assert(err, qt.IsNil)
	c.Assert(status2Value, qt.Equals, BallotStatusVerified)

	// Test 6: Clean and verify the cleanup
	// Clean again and we should get the number of entries we just added
	count2, err := st.CleanProcessBallots(pidBytes)
	c.Assert(err, qt.IsNil)
	c.Assert(count2, qt.Equals, 2, qt.Commentf("expected 2 entries to be cleaned, got %d", count2))
}
