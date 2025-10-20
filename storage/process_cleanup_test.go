package storage

import (
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
)

func TestCleanProcessStaleVotes_RemovesAll(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := []byte("p1")
	ensureProcess(t, stg, pid)

	// 1) Verified ballots (push pending -> reserve -> mark done)
	verifiedIDs := [][]byte{[]byte("vv1")}
	for _, id := range verifiedIDs {
		c.Assert(stg.PushPendingBallot(mkBallot(pid, id)), qt.IsNil)
		_, key, err := stg.NextPendingBallot()
		c.Assert(err, qt.IsNil)
		c.Assert(stg.MarkBallotVerified(key, mkVerifiedBallot(pid, id)), qt.IsNil)
	}

	// 2) Pending ballots
	pendingIDs := [][]byte{[]byte("pv1"), []byte("pv2")}
	for _, id := range pendingIDs {
		c.Assert(stg.PushPendingBallot(mkBallot(pid, id)), qt.IsNil)
	}

	// 3) Aggregator batch (ready for state transition)
	aggIDs := [][]byte{[]byte("ab1")}
	aggBallots := make([]*AggregatorBallot, 0, len(aggIDs))
	for _, id := range aggIDs {
		aggBallots = append(aggBallots, mkAggBallot(id))
	}
	c.Assert(stg.PushAggregatorBatch(&AggregatorBallotBatch{
		ProcessID: types.HexBytes(pid),
		Ballots:   aggBallots,
	}), qt.IsNil)

	// 4) State transition batch (pending state transitions)
	stIDs := [][]byte{[]byte("st1"), []byte("st2")}
	stBallots := make([]*AggregatorBallot, 0, len(stIDs))
	for _, id := range stIDs {
		stBallots = append(stBallots, mkAggBallot(id))
	}
	c.Assert(stg.PushStateTransitionBatch(&StateTransitionBatch{
		ProcessID: types.HexBytes(pid),
		Ballots:   stBallots,
	}), qt.IsNil)

	// Sanity before cleaning
	c.Assert(stg.CountPendingBallots() >= 1, qt.IsTrue)
	c.Assert(stg.CountVerifiedBallots(pid) >= 1, qt.IsTrue)

	// Act: clean process stale votes
	c.Assert(stg.CleanProcessStaleVotes(pid), qt.IsNil)

	// Assert: pending ballots removed
	c.Assert(stg.CountPendingBallots(), qt.Equals, 0)

	// Assert: verified ballots not available (no non-reserved items)
	if _, _, err := stg.PullVerifiedBallots(pid, 10); err == nil {
		t.Fatalf("expected no verified ballots available after cleanup")
	} else {
		if err != ErrNotFound {
			t.Fatalf("expected ErrNotFound for verified ballots, got: %v", err)
		}
	}

	// Assert: aggregator batches removed
	if _, _, err := stg.NextAggregatorBatch(pid); err == nil {
		t.Fatalf("expected no more aggregator batches after cleanup")
	} else if err != ErrNoMoreElements {
		t.Fatalf("expected ErrNoMoreElements for aggregator batches, got: %v", err)
	}

	// Assert: state transition batches removed
	if _, _, err := stg.NextStateTransitionBatch(pid); err == nil {
		t.Fatalf("expected no more state transition batches after cleanup")
	} else if err != ErrNoMoreElements {
		t.Fatalf("expected ErrNoMoreElements for state transition batches, got: %v", err)
	}
}
