package storage

import (
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
)

// TestVoteIDLockRelease verifies that vote ID locks are properly released
// after successful ballot processing, allowing overwrites from the same address
func TestVoteIDLockRelease(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := []byte("process1")
	address := []byte("address1")
	voteID1 := []byte("voteID1")
	voteID2 := []byte("voteID2")

	ensureProcess(t, stg, pid)

	// Create first ballot from address1 with voteID1
	ballot1 := &Ballot{
		ProcessID: types.HexBytes(pid),
		VoteID:    types.HexBytes(voteID1),
		Address:   new(big.Int).SetBytes(address),
	}

	// Push first ballot - should succeed
	err := stg.PushPendingBallot(ballot1)
	c.Assert(err, qt.IsNil)

	// Verify vote ID is locked
	c.Assert(stg.IsVoteIDProcessing(ballot1.VoteID.BigInt().MathBigInt()), qt.IsTrue)

	// Try to push same vote ID again - should fail with ErrNullifierProcessing
	err = stg.PushPendingBallot(ballot1)
	c.Assert(err, qt.Equals, ErrNullifierProcessing)

	// Process the ballot through the pipeline
	_, key, err := stg.NextPendingBallot()
	c.Assert(err, qt.IsNil)

	// Mark as verified
	verifiedBallot1 := &VerifiedBallot{
		ProcessID: types.HexBytes(pid),
		VoteID:    types.HexBytes(voteID1),
		Address:   new(big.Int).SetBytes(address),
	}
	err = stg.MarkBallotVerified(key, verifiedBallot1)
	c.Assert(err, qt.IsNil)

	// Vote ID should still be locked (in verified queue)
	c.Assert(stg.IsVoteIDProcessing(ballot1.VoteID.BigInt().MathBigInt()), qt.IsTrue)

	// Pull verified ballot
	vbs, keys, err := stg.PullVerifiedBallots(pid, 1)
	c.Assert(err, qt.IsNil)
	c.Assert(len(vbs), qt.Equals, 1)

	// Mark as done (simulating successful aggregation)
	err = stg.MarkVerifiedBallotsDone(keys...)
	c.Assert(err, qt.IsNil)

	// Vote ID lock should now be released
	c.Assert(stg.IsVoteIDProcessing(ballot1.VoteID.BigInt().MathBigInt()), qt.IsFalse,
		qt.Commentf("Vote ID lock should be released after successful aggregation"))

	// Now user should be able to submit a new vote (overwrite) with different vote ID
	ballot2 := &Ballot{
		ProcessID: types.HexBytes(pid),
		VoteID:    types.HexBytes(voteID2),
		Address:   new(big.Int).SetBytes(address), // Same address
	}

	// Push second ballot - should succeed (overwrite scenario)
	err = stg.PushPendingBallot(ballot2)
	c.Assert(err, qt.IsNil, qt.Commentf("Should allow overwrite with new vote ID after first vote is aggregated"))

	// Verify second vote ID is now locked
	c.Assert(stg.IsVoteIDProcessing(ballot2.VoteID.BigInt().MathBigInt()), qt.IsTrue)
}

// TestVoteIDLockReleaseOnFailure verifies that vote ID locks are released
// when ballot processing fails
func TestVoteIDLockReleaseOnFailure(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := []byte("process1")
	voteID := []byte("voteID1")

	ensureProcess(t, stg, pid)

	ballot := mkBallot(pid, voteID)

	// Push ballot
	err := stg.PushPendingBallot(ballot)
	c.Assert(err, qt.IsNil)

	// Verify vote ID is locked
	c.Assert(stg.IsVoteIDProcessing(ballot.VoteID.BigInt().MathBigInt()), qt.IsTrue)

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
	c.Assert(stg.IsVoteIDProcessing(ballot.VoteID.BigInt().MathBigInt()), qt.IsFalse,
		qt.Commentf("Vote ID lock should be released after failure"))
}

// TestNoDuplicateAddressesInBatch verifies that PullVerifiedBallots
// prevents multiple ballots with the same address from being in the same batch
func TestNoDuplicateAddressesInBatch(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := []byte("process1")
	address := []byte("address1")
	voteID1 := []byte("voteID1")
	voteID2 := []byte("voteID2")

	ensureProcess(t, stg, pid)

	// Create two ballots from same address with different vote IDs
	ballot1 := &Ballot{
		ProcessID: types.HexBytes(pid),
		VoteID:    types.HexBytes(voteID1),
		Address:   new(big.Int).SetBytes(address),
	}
	ballot2 := &Ballot{
		ProcessID: types.HexBytes(pid),
		VoteID:    types.HexBytes(voteID2),
		Address:   new(big.Int).SetBytes(address), // Same address
	}

	// Push both ballots
	c.Assert(stg.PushPendingBallot(ballot1), qt.IsNil)
	c.Assert(stg.PushPendingBallot(ballot2), qt.IsNil)

	// Process both to verified
	for i := 0; i < 2; i++ {
		_, key, err := stg.NextPendingBallot()
		c.Assert(err, qt.IsNil)

		var voteID []byte
		if i == 0 {
			voteID = voteID1
		} else {
			voteID = voteID2
		}

		verifiedBallot := &VerifiedBallot{
			ProcessID: types.HexBytes(pid),
			VoteID:    types.HexBytes(voteID),
			Address:   new(big.Int).SetBytes(address),
		}
		c.Assert(stg.MarkBallotVerified(key, verifiedBallot), qt.IsNil)
	}

	// Both should be in verified queue
	c.Assert(stg.CountVerifiedBallots(pid), qt.Equals, 2)

	// Pull verified ballots - should only get ONE due to address deduplication
	vbs, keys, err := stg.PullVerifiedBallots(pid, 10)
	c.Assert(err, qt.IsNil)
	c.Assert(len(vbs), qt.Equals, 1,
		qt.Commentf("Should only pull one ballot per address per batch"))

	// Verify the address is the expected one
	c.Assert(vbs[0].Address.Bytes(), qt.DeepEquals, address)

	// After marking done, the other ballot should still be available
	c.Assert(stg.MarkVerifiedBallotsDone(keys...), qt.IsNil)

	// Second ballot should now be available for next batch
	c.Assert(stg.CountVerifiedBallots(pid), qt.Equals, 1)
}

// TestMultipleOverwrites verifies that multiple overwrites work correctly
func TestMultipleOverwrites(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := []byte("process1")
	address := []byte("address1")

	ensureProcess(t, stg, pid)

	// Simulate 3 overwrites from the same address
	for i := 1; i <= 3; i++ {
		voteID := []byte{byte(i)}

		ballot := &Ballot{
			ProcessID: types.HexBytes(pid),
			VoteID:    types.HexBytes(voteID),
			Address:   new(big.Int).SetBytes(address),
		}

		// Push ballot
		err := stg.PushPendingBallot(ballot)
		c.Assert(err, qt.IsNil, qt.Commentf("Overwrite %d should succeed", i))

		// Process to verified
		_, key, err := stg.NextPendingBallot()
		c.Assert(err, qt.IsNil)

		verifiedBallot := &VerifiedBallot{
			ProcessID: types.HexBytes(pid),
			VoteID:    types.HexBytes(voteID),
			Address:   new(big.Int).SetBytes(address),
		}
		c.Assert(stg.MarkBallotVerified(key, verifiedBallot), qt.IsNil)

		// Pull and mark done
		_, keys, err := stg.PullVerifiedBallots(pid, 1)
		c.Assert(err, qt.IsNil)
		c.Assert(stg.MarkVerifiedBallotsDone(keys...), qt.IsNil)

		// Verify lock is released
		c.Assert(stg.IsVoteIDProcessing(ballot.VoteID.BigInt().MathBigInt()), qt.IsFalse,
			qt.Commentf("Vote ID lock should be released after overwrite %d", i))
	}
}
