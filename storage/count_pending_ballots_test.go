package storage

import (
	"math/big"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/metadb"
)

func TestCountPendingBallotsBasic(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	processID := types.ProcessID{
		Address: common.Address{},
		Nonce:   0,
		ChainID: 0,
	}

	// Test: No ballots initially
	c.Assert(st.CountPendingBallots(), qt.Equals, 0, qt.Commentf("no pending ballots expected initially"))

	// Create ballot with unique BallotInputsHash
	ballot := &Ballot{
		ProcessID:        processID.Marshal(),
		Nullifier:        big.NewInt(1111),
		Address:          big.NewInt(2222),
		BallotInputsHash: big.NewInt(3333),
	}

	// Test: Add one ballot
	c.Assert(st.PushBallot(ballot), qt.IsNil)
	c.Assert(st.CountPendingBallots(), qt.Equals, 1, qt.Commentf("should have 1 pending ballot"))

	// Test: Reserve the ballot
	b1, b1key, err := st.NextBallot()
	c.Assert(err, qt.IsNil, qt.Commentf("should retrieve a ballot"))
	c.Assert(b1, qt.IsNotNil)
	c.Assert(b1key, qt.IsNotNil)
	c.Assert(st.CountPendingBallots(), qt.Equals, 0, qt.Commentf("should have 0 pending ballots after reservation"))

	// Test: Mark ballot done - removes it completely
	verified := &VerifiedBallot{
		ProcessID:   processID.Marshal(),
		Nullifier:   b1.Nullifier,
		VoterWeight: big.NewInt(42),
	}
	c.Assert(st.MarkBallotDone(b1key, verified), qt.IsNil)
	c.Assert(st.CountPendingBallots(), qt.Equals, 0, qt.Commentf("should have 0 pending ballots after marking as done"))

	// Test: No more ballots available
	_, _, err = st.NextBallot()
	c.Assert(err, qt.Equals, ErrNoMoreElements, qt.Commentf("no more ballots expected"))
}

func TestCountPendingBallotsReservations(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	processID := types.ProcessID{
		Address: common.Address{},
		Nonce:   0,
		ChainID: 0,
	}

	// Test: Add 3 ballots with well-separated BallotInputsHash values to avoid conflicts
	ballot1 := &Ballot{
		ProcessID:        processID.Marshal(),
		Nullifier:        big.NewInt(10000),
		Address:          big.NewInt(20000),
		BallotInputsHash: big.NewInt(30000),
	}
	ballot2 := &Ballot{
		ProcessID:        processID.Marshal(),
		Nullifier:        big.NewInt(10001),
		Address:          big.NewInt(20001),
		BallotInputsHash: big.NewInt(30001),
	}
	ballot3 := &Ballot{
		ProcessID:        processID.Marshal(),
		Nullifier:        big.NewInt(10002),
		Address:          big.NewInt(20002),
		BallotInputsHash: big.NewInt(30002),
	}

	c.Assert(st.PushBallot(ballot1), qt.IsNil)
	c.Assert(st.CountPendingBallots(), qt.Equals, 1, qt.Commentf("should have 1 pending ballot"))

	c.Assert(st.PushBallot(ballot2), qt.IsNil)
	c.Assert(st.CountPendingBallots(), qt.Equals, 2, qt.Commentf("should have 2 pending ballots"))

	c.Assert(st.PushBallot(ballot3), qt.IsNil)
	c.Assert(st.CountPendingBallots(), qt.Equals, 3, qt.Commentf("should have 3 pending ballots"))

	// Test: Reserve one ballot - count should decrease
	b1, b1key, err := st.NextBallot()
	c.Assert(err, qt.IsNil, qt.Commentf("should retrieve first ballot"))
	c.Assert(b1, qt.IsNotNil)
	c.Assert(b1key, qt.IsNotNil)

	// Verify count decreased due to reservation
	count := st.CountPendingBallots()
	c.Assert(count, qt.Equals, 2, qt.Commentf("should have 2 pending ballots after reserving one"))

	// Test: Reserve second ballot
	b2, b2key, err := st.NextBallot()
	c.Assert(err, qt.IsNil, qt.Commentf("should retrieve second ballot"))
	c.Assert(b2, qt.IsNotNil)
	c.Assert(b2key, qt.IsNotNil)

	// Verify count decreased again
	count = st.CountPendingBallots()
	c.Assert(count, qt.Equals, 1, qt.Commentf("should have 1 pending ballot after reserving two"))

	// Test: Reserve third ballot
	b3, b3key, err := st.NextBallot()
	c.Assert(err, qt.IsNil, qt.Commentf("should retrieve third ballot"))
	c.Assert(b3, qt.IsNotNil)
	c.Assert(b3key, qt.IsNotNil)

	// Verify no pending ballots remain (all reserved)
	count = st.CountPendingBallots()
	c.Assert(count, qt.Equals, 0, qt.Commentf("should have 0 pending ballots after reserving all"))

	// Test: No more ballots available
	_, _, err = st.NextBallot()
	c.Assert(err, qt.Equals, ErrNoMoreElements, qt.Commentf("no more ballots expected"))
}
