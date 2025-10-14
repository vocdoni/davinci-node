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
		Address:   new(big.Int).SetBytes(append(pid, id...)),
	}
}

func mkVerifiedBallot(pid, id []byte) *VerifiedBallot {
	return &VerifiedBallot{
		ProcessID: types.HexBytes(pid),
		VoteID:    types.HexBytes(id),
		Address:   new(big.Int).SetBytes(append(pid, id...)),
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

func TestBallotQueue_RemovePendingBallotsByProcess_RemovesAllPendingCurrently(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid1 := []byte("p1")
	pid2 := []byte("p2")
	ids := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	ensureProcess(t, stg, pid1)
	ensureProcess(t, stg, pid2)

	c.Assert(stg.PushPendingBallot(mkBallot(pid1, ids[0])), qt.IsNil)
	c.Assert(stg.PushPendingBallot(mkBallot(pid1, ids[1])), qt.IsNil)
	c.Assert(stg.PushPendingBallot(mkBallot(pid2, ids[2])), qt.IsNil)

	// remove pending ballots "by process" (but currently removes all)
	err := stg.RemovePendingBallotsByProcess(pid1)
	c.Assert(err, qt.IsNil)

	// no pending should remain
	_, _, err = stg.NextPendingBallot()
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

	c.Assert(stg.PushPendingBallot(mkBallot(pid, id1)), qt.IsNil)
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

	pid := []byte("p1")
	id1 := []byte("id1")
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

	pid1 := []byte("p1")
	pid2 := []byte("p2")
	ensureProcess(t, stg, pid1)
	ensureProcess(t, stg, pid2)

	// create verified for pid1 and pid2
	for i := range 2 {
		id := []byte(fmt.Sprintf("a%d", i))
		c.Assert(stg.PushPendingBallot(mkBallot(pid1, id)), qt.IsNil)
		_, key, err := stg.NextPendingBallot()
		c.Assert(err, qt.IsNil)
		c.Assert(stg.MarkBallotVerified(key, mkVerifiedBallot(pid1, id)), qt.IsNil)
	}
	for i := range 1 {
		id := []byte(fmt.Sprintf("b%d", i))
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

	pid := []byte("p1")
	ids := [][]byte{[]byte("a"), []byte("b")}
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
