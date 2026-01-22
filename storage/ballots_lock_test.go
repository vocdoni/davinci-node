package storage

import (
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/internal/testutil"
)

// TestVoteIDLockRelease verifies that vote ID locks are properly released
// after successful ballot processing, allowing overwrites from the same address
func TestVoteIDLockRelease(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := testutil.RandomProcessID()
	address := []byte("address1")
	voteID1 := testutil.RandomVoteID()
	voteID2 := testutil.RandomVoteID()

	ensureProcess(t, stg, pid)

	// Create first ballot from address1 with voteID1
	ballot1 := &Ballot{
		ProcessID: pid,
		VoteID:    voteID1,
		Address:   new(big.Int).SetBytes(address),
	}

	// Push first ballot - should succeed
	err := stg.PushPendingBallot(ballot1)
	c.Assert(err, qt.IsNil)

	// Verify vote ID is locked
	c.Assert(stg.IsVoteIDProcessing(ballot1.VoteID), qt.IsTrue)

	// Try to push same vote ID again - should fail with ErrNullifierProcessing
	err = stg.PushPendingBallot(ballot1)
	c.Assert(err, qt.Equals, ErrNullifierProcessing)

	// Process the ballot through the pipeline
	_, key, err := stg.NextPendingBallot()
	c.Assert(err, qt.IsNil)

	// Mark as verified
	verifiedBallot1 := &VerifiedBallot{
		ProcessID: pid,
		VoteID:    voteID1,
		Address:   new(big.Int).SetBytes(address),
	}
	err = stg.MarkBallotVerified(key, verifiedBallot1)
	c.Assert(err, qt.IsNil)

	// Vote ID should still be locked (in verified queue)
	c.Assert(stg.IsVoteIDProcessing(ballot1.VoteID), qt.IsTrue)

	// Pull verified ballot
	vbs, keys, err := stg.PullVerifiedBallots(pid, 1)
	c.Assert(err, qt.IsNil)
	c.Assert(len(vbs), qt.Equals, 1)

	// Mark as done (simulating successful aggregation)
	err = stg.MarkVerifiedBallotsDone(keys...)
	c.Assert(err, qt.IsNil)

	// Vote ID lock should now be released
	c.Assert(stg.IsVoteIDProcessing(ballot1.VoteID), qt.IsFalse,
		qt.Commentf("Vote ID lock should be released after successful aggregation"))

	// Now user should be able to submit a new vote (overwrite) with different vote ID
	ballot2 := &Ballot{
		ProcessID: pid,
		VoteID:    voteID2,
		Address:   new(big.Int).SetBytes(address), // Same address
	}

	// Push second ballot - should succeed (overwrite scenario)
	err = stg.PushPendingBallot(ballot2)
	c.Assert(err, qt.IsNil, qt.Commentf("Should allow overwrite with new vote ID after first vote is aggregated"))

	// Verify second vote ID is now locked
	c.Assert(stg.IsVoteIDProcessing(ballot2.VoteID), qt.IsTrue)
}

// TestVoteIDLockReleaseOnFailure verifies that vote ID locks are released
// when ballot processing fails
func TestVoteIDLockReleaseOnFailure(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := testutil.RandomProcessID()
	voteID := testutil.RandomVoteID()

	ensureProcess(t, stg, pid)

	ballot := mkBallot(pid, voteID)

	// Push ballot
	err := stg.PushPendingBallot(ballot)
	c.Assert(err, qt.IsNil)

	// Verify vote ID is locked
	c.Assert(stg.IsVoteIDProcessing(ballot.VoteID), qt.IsTrue)

	// Process through to verified
	_, key, err := stg.NextPendingBallot()
	c.Assert(err, qt.IsNil)

	verifiedBallot := mkVerifiedBallot(pid, voteID)
	err = stg.MarkBallotVerified(key, verifiedBallot)
	c.Assert(err, qt.IsNil)

	// Pull verified ballot
	_, keys, err := stg.PullVerifiedBallots(pid, 1)
	c.Assert(err, qt.IsNil)

	// Mark as failed
	err = stg.MarkVerifiedBallotsFailed(keys...)
	c.Assert(err, qt.IsNil)

	// Vote ID lock should be released on failure
	c.Assert(stg.IsVoteIDProcessing(ballot.VoteID), qt.IsFalse,
		qt.Commentf("Vote ID lock should be released after failure"))
}

// TestNoDuplicateAddressesInBatch verifies that the address locking prevents
// multiple ballots from the same address from being submitted simultaneously
func TestNoDuplicateAddressesInBatch(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := testutil.RandomProcessID()
	address := []byte("address1")
	voteID1 := testutil.RandomVoteID()
	voteID2 := testutil.RandomVoteID()

	ensureProcess(t, stg, pid)

	// Create two ballots from same address with different vote IDs
	ballot1 := &Ballot{
		ProcessID: pid,
		VoteID:    voteID1,
		Address:   new(big.Int).SetBytes(address),
	}
	ballot2 := &Ballot{
		ProcessID: pid,
		VoteID:    voteID2,
		Address:   new(big.Int).SetBytes(address), // Same address
	}

	// Push first ballot - should succeed
	c.Assert(stg.PushPendingBallot(ballot1), qt.IsNil)

	// Try to push second ballot from same address - should fail with ErrAddressProcessing
	err := stg.PushPendingBallot(ballot2)
	c.Assert(err, qt.Equals, ErrAddressProcessing,
		qt.Commentf("Should reject second ballot from same address while first is processing"))

	// Process first ballot through to aggregation
	_, key, err := stg.NextPendingBallot()
	c.Assert(err, qt.IsNil)

	verifiedBallot := &VerifiedBallot{
		ProcessID: pid,
		VoteID:    voteID1,
		Address:   new(big.Int).SetBytes(address),
	}
	c.Assert(stg.MarkBallotVerified(key, verifiedBallot), qt.IsNil)

	// Pull and mark done (simulating aggregation)
	vbs, keys, err := stg.PullVerifiedBallots(pid, 10)
	c.Assert(err, qt.IsNil)
	c.Assert(len(vbs), qt.Equals, 1)
	c.Assert(stg.MarkVerifiedBallotsDone(keys...), qt.IsNil)

	// Now second ballot should be accepted (overwrite scenario)
	// Note: We need to create a fresh ballot object since the previous push attempt
	// may have left some state
	ballot2Fresh := &Ballot{
		ProcessID: pid,
		VoteID:    voteID2,
		Address:   new(big.Int).SetBytes(address),
	}
	c.Assert(stg.PushPendingBallot(ballot2Fresh), qt.IsNil,
		qt.Commentf("Should allow overwrite after first ballot is aggregated"))
}

// TestMultipleOverwrites verifies that multiple overwrites work correctly
func TestMultipleOverwrites(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := testutil.RandomProcessID()
	address := []byte("address1")

	ensureProcess(t, stg, pid)

	// Simulate 3 overwrites from the same address
	for i := 1; i <= 3; i++ {
		voteID := testutil.RandomVoteID()

		ballot := &Ballot{
			ProcessID: pid,
			VoteID:    voteID,
			Address:   new(big.Int).SetBytes(address),
		}

		// Push ballot
		err := stg.PushPendingBallot(ballot)
		c.Assert(err, qt.IsNil, qt.Commentf("Overwrite %d should succeed", i))

		// Process to verified
		_, key, err := stg.NextPendingBallot()
		c.Assert(err, qt.IsNil)

		verifiedBallot := &VerifiedBallot{
			ProcessID: pid,
			VoteID:    voteID,
			Address:   new(big.Int).SetBytes(address),
		}
		c.Assert(stg.MarkBallotVerified(key, verifiedBallot), qt.IsNil)

		// Pull and mark done
		_, keys, err := stg.PullVerifiedBallots(pid, 1)
		c.Assert(err, qt.IsNil)
		c.Assert(stg.MarkVerifiedBallotsDone(keys...), qt.IsNil)

		// Verify lock is released
		c.Assert(stg.IsVoteIDProcessing(ballot.VoteID), qt.IsFalse,
			qt.Commentf("Vote ID lock should be released after overwrite %d", i))
	}
}
