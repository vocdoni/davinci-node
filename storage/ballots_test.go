package storage

import (
	"errors"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/types"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	dbDir := t.TempDir()
	testdb, err := metadb.New(db.TypePebble, dbDir)
	if err != nil {
		t.Fatalf("metadb.New: %v", err)
	}
	return New(testdb)
}

func mkBallot(pid types.ProcessID, voteID types.VoteID) *Ballot {
	return &Ballot{
		ProcessID: pid,
		VoteID:    voteID,
		Address:   testutil.DeterministicAddress(voteID.Uint64()).Big(),
	}
}

func mkVerifiedBallot(pid types.ProcessID, voteID types.VoteID) *VerifiedBallot {
	return &VerifiedBallot{
		ProcessID: pid,
		VoteID:    voteID,
		Address:   testutil.DeterministicAddress(voteID.Uint64()).Big(),
	}
}

func mkAggBallot(voteID types.VoteID) *AggregatorBallot {
	return &AggregatorBallot{
		VoteID: voteID,
	}
}

func ensureProcess(t *testing.T, stg *Storage, pid types.ProcessID) {
	t.Helper()
	censusRoot := make([]byte, types.CensusRootLength)
	proc := &types.Process{
		ID:         &pid,
		Status:     types.ProcessStatusReady,
		BallotMode: testutil.BallotMode(),
		Census: &types.Census{
			CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
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

	pid := testutil.RandomProcessID()
	id1 := testutil.RandomVoteID()
	ensureProcess(t, stg, pid)

	// push a pending ballot
	err := stg.PushPendingBallot(mkBallot(pid, id1))
	c.Assert(err, qt.IsNil)

	// remove it
	err = stg.RemovePendingBallot(pid, id1)
	c.Assert(err, qt.IsNil)

	// no more pending ballots
	_, _, err = stg.NextPendingBallot()
	c.Assert(errors.Is(err, ErrNoMoreElements), qt.IsTrue)

	// status should be error
	status, err := stg.VoteIDStatus(pid, id1)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusError)
}

func TestBallotQueue_RemovePendingBallotsByProcess_OnlyRemovesTargetProcess(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid1 := testutil.RandomProcessID()
	pid2 := testutil.RandomProcessID()
	ids := []types.VoteID{
		testutil.RandomVoteID(),
		testutil.RandomVoteID(),
		testutil.RandomVoteID(),
	}
	ensureProcess(t, stg, pid1)
	ensureProcess(t, stg, pid2)

	c.Assert(stg.PushPendingBallot(mkBallot(pid1, ids[0])), qt.IsNil)
	c.Assert(stg.PushPendingBallot(mkBallot(pid1, ids[1])), qt.IsNil)
	c.Assert(stg.PushPendingBallot(mkBallot(pid2, ids[2])), qt.IsNil)

	// remove pending ballots for pid1 only
	err := stg.RemovePendingBallotsByProcess(pid1)
	c.Assert(err, qt.IsNil)

	// pid2's ballot should still remain
	c.Assert(stg.CountPendingBallots(), qt.Equals, 1)

	// verify the remaining ballot belongs to pid2
	ballot, _, err := stg.NextPendingBallot()
	c.Assert(err, qt.IsNil)
	c.Assert(ballot.ProcessID.Bytes(), qt.DeepEquals, pid2.Bytes())
	c.Assert(ballot.VoteID, qt.DeepEquals, ids[2])

	// verify pid1's ballots still have pending status (removePendingBallot doesn't change status)
	status1, err := stg.VoteIDStatus(pid1, ids[0])
	c.Assert(err, qt.IsNil)
	c.Assert(status1, qt.Equals, VoteIDStatusPending)

	status2, err := stg.VoteIDStatus(pid1, ids[1])
	c.Assert(err, qt.IsNil)
	c.Assert(status2, qt.Equals, VoteIDStatusPending)

	// verify pid2's ballot still has pending status
	status, err := stg.VoteIDStatus(pid2, ids[2])
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, VoteIDStatusPending)
}

func TestBallotQueue_ReleaseBallotReservation(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := testutil.RandomProcessID()
	ensureProcess(t, stg, pid)

	c.Assert(stg.PushPendingBallot(mkBallot(pid, testutil.RandomVoteID())), qt.IsNil)
	c.Assert(stg.CountPendingBallots(), qt.Equals, 1)

	// reserve the ballot
	_, key, err := stg.NextPendingBallot()
	c.Assert(err, qt.IsNil)
	c.Assert(stg.CountPendingBallots(), qt.Equals, 0)

	// release the reservation
	err = stg.ReleasePendingBallotReservation(key)
	c.Assert(err, qt.IsNil)
	c.Assert(stg.CountPendingBallots(), qt.Equals, 1)
}

func TestBallotQueue_MarkBallotDoneAndPullVerified(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := testutil.RandomProcessID()
	id1 := testutil.RandomVoteID()
	ensureProcess(t, stg, pid)

	c.Assert(stg.PushPendingBallot(mkBallot(pid, id1)), qt.IsNil)

	// reserve pending ballot
	_, key, err := stg.NextPendingBallot()
	c.Assert(err, qt.IsNil)

	// mark done - move to verified queue
	vb := mkVerifiedBallot(pid, id1)
	err = stg.MarkBallotVerified(key, vb)
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
	c.Assert(vbs[0].ProcessID.Bytes(), qt.DeepEquals, pid.Bytes())
	c.Assert(vbs[0].VoteID, qt.DeepEquals, id1)

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

	pid := testutil.RandomProcessID()
	ids := []types.VoteID{
		testutil.RandomVoteID(),
		testutil.RandomVoteID(),
		testutil.RandomVoteID(),
	}
	ensureProcess(t, stg, pid)

	// create 3 verified ballots via Push + Next + MarkDone
	for _, id := range ids {
		c.Assert(stg.PushPendingBallot(mkBallot(pid, id)), qt.IsNil)
		_, key, err := stg.NextPendingBallot()
		c.Assert(err, qt.IsNil)
		c.Assert(stg.MarkBallotVerified(key, mkVerifiedBallot(pid, id)), qt.IsNil)
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

	pid1 := testutil.RandomProcessID()
	pid2 := testutil.RandomProcessID()
	ensureProcess(t, stg, pid1)
	ensureProcess(t, stg, pid2)

	// create verified for pid1 and pid2
	for range 2 {
		id := testutil.RandomVoteID()
		c.Assert(stg.PushPendingBallot(mkBallot(pid1, id)), qt.IsNil)
		_, key, err := stg.NextPendingBallot()
		c.Assert(err, qt.IsNil)
		c.Assert(stg.MarkBallotVerified(key, mkVerifiedBallot(pid1, id)), qt.IsNil)
	}
	for range 1 {
		id := testutil.RandomVoteID()
		c.Assert(stg.PushPendingBallot(mkBallot(pid2, id)), qt.IsNil)
		_, key, err := stg.NextPendingBallot()
		c.Assert(err, qt.IsNil)
		c.Assert(stg.MarkBallotVerified(key, mkVerifiedBallot(pid2, id)), qt.IsNil)
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

	pid := testutil.RandomProcessID()
	ids := []types.VoteID{
		testutil.RandomVoteID(),
		testutil.RandomVoteID(),
	}
	ensureProcess(t, stg, pid)

	// create verified ballots
	for _, id := range ids {
		c.Assert(stg.PushPendingBallot(mkBallot(pid, id)), qt.IsNil)
		_, key, err := stg.NextPendingBallot()
		c.Assert(err, qt.IsNil)
		c.Assert(stg.MarkBallotVerified(key, mkVerifiedBallot(pid, id)), qt.IsNil)
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
