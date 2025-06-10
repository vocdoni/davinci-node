package storage

import (
	"bytes"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/metadb"
)

// createTestProcess creates a standard test process with the given process ID
func createTestProcess(pid *types.ProcessID) *types.Process {
	return &types.Process{
		ID:               pid.Marshal(),
		Status:           0,
		StartTime:        time.Now(),
		Duration:         time.Hour,
		MetadataURI:      "http://example.com/metadata",
		StateRoot:        new(types.BigInt).SetUint64(100),
		SequencerStats:   types.SequencerProcessStats{},
		IsAcceptingVotes: true,
		BallotMode: &types.BallotMode{
			MaxCount:     8,
			MaxValue:     new(types.BigInt).SetUint64(100),
			MinValue:     new(types.BigInt).SetUint64(0),
			MaxTotalCost: new(types.BigInt).SetUint64(0),
			MinTotalCost: new(types.BigInt).SetUint64(0),
		},
		Census: &types.Census{
			CensusRoot: make([]byte, 32),
			MaxVotes:   new(types.BigInt).SetUint64(1000),
			CensusURI:  "http://example.com/census",
		},
	}
}

func TestBallotQueue(t *testing.T) {
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

	// Create the process first
	err = st.SetProcess(createTestProcess(&processID))
	c.Assert(err, qt.IsNil)

	// Scenario: No ballots initially
	c.Assert(st.CountPendingBallots(), qt.Equals, 0, qt.Commentf("no pending ballots expected initially"))
	_, _, err = st.NextBallot()
	c.Assert(err, qt.Equals, ErrNoMoreElements, qt.Commentf("no ballots expected initially"))

	// Create ballots with fixed data for deterministic testing
	ballot1 := &Ballot{
		ProcessID:        processID.Marshal(),
		Nullifier:        new(big.Int).SetBytes(bytes.Repeat([]byte{1}, 32)),
		Address:          new(big.Int).SetBytes(bytes.Repeat([]byte{1}, 20)),
		BallotInputsHash: new(big.Int).SetBytes(bytes.Repeat([]byte{1}, 32)),
	}
	ballot2 := &Ballot{
		ProcessID:        processID.Marshal(),
		Nullifier:        new(big.Int).SetBytes(bytes.Repeat([]byte{2}, 32)),
		Address:          new(big.Int).SetBytes(bytes.Repeat([]byte{2}, 20)),
		BallotInputsHash: new(big.Int).SetBytes(bytes.Repeat([]byte{2}, 32)),
	}

	// Push the ballots
	c.Assert(st.PushBallot(ballot1), qt.IsNil)
	c.Assert(st.PushBallot(ballot2), qt.IsNil)

	// Verify count of pending ballots
	c.Assert(st.CountPendingBallots(), qt.Equals, 2, qt.Commentf("should have 2 pending ballots after pushing"))

	// Fetch next ballot and verify its content
	b1, b1key, err := st.NextBallot()
	c.Assert(err, qt.IsNil, qt.Commentf("should retrieve a ballot"))
	c.Assert(b1, qt.IsNotNil)
	c.Assert(b1key, qt.IsNotNil)

	// Verify count decreased due to reservation
	c.Assert(st.CountPendingBallots(), qt.Equals, 1, qt.Commentf("should have 1 pending ballot after reserving one"))

	// Store the first ballot's nullifier to track which one we got
	firstNullifier := b1.Nullifier.String()

	// Mark the first ballot done, provide a verified ballot
	verified1 := &VerifiedBallot{
		ProcessID:   processID.Marshal(),
		Nullifier:   b1.Nullifier,
		VoterWeight: big.NewInt(42),
	}
	c.Assert(st.MarkBallotDone(b1key, verified1), qt.IsNil)

	// Fetch the second ballot
	b2, b2key, err := st.NextBallot()
	c.Assert(err, qt.IsNil, qt.Commentf("should retrieve second ballot"))
	c.Assert(b2, qt.IsNotNil)
	c.Assert(b2key, qt.IsNotNil)

	// Verify no more pending ballots (both are reserved)
	c.Assert(st.CountPendingBallots(), qt.Equals, 0, qt.Commentf("should have 0 pending ballots after reserving both"))

	// Verify we got a different ballot than the first one
	c.Assert(
		b2.Nullifier.String(),
		qt.Not(qt.Equals),
		firstNullifier,
		qt.Commentf("second ballot should be different from first"),
	)

	// Mark second ballot done as well
	verified2 := &VerifiedBallot{
		ProcessID:   processID.Marshal(),
		Nullifier:   b2.Nullifier,
		VoterWeight: big.NewInt(24),
	}
	c.Assert(st.MarkBallotDone(b2key, verified2), qt.IsNil)

	// There should be now 2 verified ballots.
	c.Assert(st.CountVerifiedBallots(
		processID.Marshal()),
		qt.Equals,
		2,
		qt.Commentf("should have 2 verified ballots"),
	)

	// Now pull verified ballots for the process
	// Test PullVerifiedBallots with different maxCount values

	// Test maxCount = 1 should return only one ballot
	vbs1, keys1, err := st.PullVerifiedBallots(processID.Marshal(), 1)
	c.Assert(err, qt.IsNil, qt.Commentf("must pull verified ballots with maxCount=2"))
	c.Assert(len(vbs1), qt.Equals, 1, qt.Commentf("should return exactly 1 ballot"))
	c.Assert(len(keys1), qt.Equals, 1, qt.Commentf("should return exactly 1 key"))

	// Verify reservation was created
	c.Assert(st.isReserved(verifiedBallotReservPrefix, keys1[0]), qt.IsTrue, qt.Commentf("ballot should be reserved"))

	// Mark first ballot as done
	c.Assert(st.MarkVerifiedBallotsDone(keys1[0]), qt.IsNil)

	// Now we should be able to pull the second ballot
	vbs3, keys3, err := st.PullVerifiedBallots(processID.Marshal(), 2)
	c.Assert(err, qt.IsNil, qt.Commentf("must pull verified ballots after marking first as done"))
	c.Assert(len(vbs3), qt.Equals, 1, qt.Commentf("should return exactly 1 ballot"))
	c.Assert(len(keys3), qt.Equals, 1, qt.Commentf("should return exactly 1 key"))

	// Verify the second ballot is now reserved
	c.Assert(st.isReserved(verifiedBallotReservPrefix, keys3[0]), qt.IsTrue, qt.Commentf("second ballot should be reserved"))

	// Test maxCount = 0 should return no ballots
	vbs0, keys0, err := st.PullVerifiedBallots(processID.Marshal(), 0)
	c.Assert(err, qt.IsNil, qt.Commentf("must pull verified ballots with maxCount=0"))
	c.Assert(len(vbs0), qt.Equals, 0, qt.Commentf("should return no ballots"))
	c.Assert(len(keys0), qt.Equals, 0, qt.Commentf("should return no keys"))

	// Test maxCount > number of available ballots should return remaining unreserved ballots
	vbs10, keys10, err := st.PullVerifiedBallots(processID.Marshal(), 10)
	c.Assert(err, qt.Equals, ErrNotFound, qt.Commentf("should return ErrNotFound when no unreserved ballots"))
	c.Assert(vbs10, qt.IsNil)
	c.Assert(keys10, qt.IsNil)

	// Try again NextBallot. There should be no more ballots.
	_, _, err = st.NextBallot()
	c.Assert(err, qt.Equals, ErrNoMoreElements, qt.Commentf("no more ballots expected"))

	// Additional scenario: MarkBallotDone on a non-existent/reserved key
	nonExistentKey := []byte("fakekey")
	err = st.MarkBallotDone(nonExistentKey, verified1)
	c.Assert(err, qt.IsNil)

	// Additional scenario: no verified ballots if none processed
	anotherPID := types.ProcessID{
		Address: common.Address{},
		ChainID: 0,
		Nonce:   999,
	}
	vbsEmpty, keysEmpty, err := st.PullVerifiedBallots(anotherPID.Marshal(), 10)
	c.Assert(err, qt.Equals, ErrNotFound, qt.Commentf("no verified ballots for a new process"))
	c.Assert(vbsEmpty, qt.IsNil)
	c.Assert(keysEmpty, qt.IsNil)
}

// TestPullVerifiedBallotsReservation specifically tests that PullVerifiedBallots
// correctly handles reservations and doesn't return the same ballots in subsequent calls.
func TestPullVerifiedBallotsReservation(t *testing.T) {
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

	// Create the process first
	err = st.SetProcess(createTestProcess(&processID))
	c.Assert(err, qt.IsNil)

	// Create 5 ballots with fixed data for deterministic testing
	for i := range 5 {
		ballot := &Ballot{
			ProcessID:        processID.Marshal(),
			Nullifier:        new(big.Int).SetBytes(bytes.Repeat([]byte{byte(i + 1)}, 32)),
			Address:          new(big.Int).SetBytes(bytes.Repeat([]byte{byte(i + 1)}, 20)),
			BallotInputsHash: new(big.Int).SetBytes(bytes.Repeat([]byte{byte(i + 1)}, 32)),
		}
		c.Assert(st.PushBallot(ballot), qt.IsNil)
	}

	// Process all ballots and convert them to verified ballots
	for i := 0; i < 5; i++ {
		b, key, err := st.NextBallot()
		c.Assert(err, qt.IsNil)
		c.Assert(b, qt.IsNotNil)

		verified := &VerifiedBallot{
			ProcessID:   processID.Marshal(),
			Nullifier:   b.Nullifier,
			VoterWeight: big.NewInt(int64(i+1) * 10), // Different weights for identification
		}
		c.Assert(st.MarkBallotDone(key, verified), qt.IsNil)
	}

	// Verify we have 5 verified ballots
	c.Assert(st.CountVerifiedBallots(processID.Marshal()), qt.Equals, 5)

	// Test 1: Pull 2 ballots
	vbs1, keys1, err := st.PullVerifiedBallots(processID.Marshal(), 2)
	c.Assert(err, qt.IsNil)
	c.Assert(len(vbs1), qt.Equals, 2)
	c.Assert(len(keys1), qt.Equals, 2)

	// Store the weights to identify these ballots
	weights1 := []int64{vbs1[0].VoterWeight.Int64(), vbs1[1].VoterWeight.Int64()}

	// Test 2: Pull 2 more ballots - should get different ones
	vbs2, keys2, err := st.PullVerifiedBallots(processID.Marshal(), 2)
	c.Assert(err, qt.IsNil)
	c.Assert(len(vbs2), qt.Equals, 2)
	c.Assert(len(keys2), qt.Equals, 2)

	// Verify the second pull returned different ballots than the first
	weights2 := []int64{vbs2[0].VoterWeight.Int64(), vbs2[1].VoterWeight.Int64()}
	for _, w1 := range weights1 {
		for _, w2 := range weights2 {
			c.Assert(w1, qt.Not(qt.Equals), w2, qt.Commentf("second pull returned a ballot from the first pull"))
		}
	}

	// Test 3: Pull 2 more ballots - should get only 1 remaining
	vbs3, keys3, err := st.PullVerifiedBallots(processID.Marshal(), 2)
	c.Assert(err, qt.IsNil)
	c.Assert(len(vbs3), qt.Equals, 1)
	c.Assert(len(keys3), qt.Equals, 1)

	// Verify the third pull returned a different ballot than the previous pulls
	weight3 := vbs3[0].VoterWeight.Int64()
	for _, w1 := range weights1 {
		c.Assert(weight3, qt.Not(qt.Equals), w1)
	}
	for _, w2 := range weights2 {
		c.Assert(weight3, qt.Not(qt.Equals), w2)
	}

	// Test 4: Pull again - should get ErrNotFound as all ballots are reserved
	vbs4, keys4, err := st.PullVerifiedBallots(processID.Marshal(), 2)
	c.Assert(err, qt.Equals, ErrNotFound)
	c.Assert(vbs4, qt.IsNil)
	c.Assert(keys4, qt.IsNil)

	// Test 5: Mark one ballot as done and pull again - should get nothing as we need to release the reservation
	c.Assert(st.MarkVerifiedBallotsDone(keys1[0]), qt.IsNil)

	// Verify count is now 0 because all ballots are either reserved or marked done
	// When a ballot is marked done, it's completely removed from the database
	c.Assert(st.CountVerifiedBallots(processID.Marshal()), qt.Equals, 0)

	// Pull again - should still get ErrNotFound as all remaining ballots are still reserved
	vbs5, keys5, err := st.PullVerifiedBallots(processID.Marshal(), 2)
	c.Assert(err, qt.Equals, ErrNotFound)
	c.Assert(vbs5, qt.IsNil)
	c.Assert(keys5, qt.IsNil)

	// Test 6: Release all reservations by clearing them (simulating a restart)
	c.Assert(st.recover(), qt.IsNil)

	// Now we should be able to pull the remaining 4 ballots
	vbs6, keys6, err := st.PullVerifiedBallots(processID.Marshal(), 5)
	c.Assert(err, qt.IsNil)
	c.Assert(len(vbs6), qt.Equals, 4)
	c.Assert(len(keys6), qt.Equals, 4)
}

func TestBallotBatchQueue(t *testing.T) {
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

	// Create the process first
	err = st.SetProcess(createTestProcess(&processID))
	c.Assert(err, qt.IsNil)

	// Test 1: Empty state
	_, _, err = st.NextBallotBatch(processID.Marshal())
	c.Assert(err, qt.Equals, ErrNoMoreElements, qt.Commentf("no batches expected initially"))

	// Test 2: Single batch lifecycle
	batch1 := &AggregatorBallotBatch{
		ProcessID: processID.Marshal(),
		Ballots: []*AggregatorBallot{
			{
				Nullifier:  new(big.Int).SetBytes(bytes.Repeat([]byte{1}, 32)),
				Address:    new(big.Int).SetBytes(bytes.Repeat([]byte{1}, 20)),
				Commitment: new(big.Int).SetBytes(bytes.Repeat([]byte{1}, 32)),
			},
		},
	}

	// Push batch and wait a moment to ensure different timestamps
	c.Assert(st.PushBallotBatch(batch1), qt.IsNil)

	// Get batch
	b1, b1key, err := st.NextBallotBatch(processID.Marshal())
	c.Assert(err, qt.IsNil, qt.Commentf("should retrieve the batch"))
	c.Assert(b1, qt.IsNotNil)
	c.Assert(len(b1.Ballots), qt.Equals, 1)
	c.Assert(b1.Ballots[0].Nullifier.Cmp(batch1.Ballots[0].Nullifier), qt.Equals, 0)

	// Mark batch done and wait a moment
	c.Assert(st.MarkBallotBatchDone(b1key), qt.IsNil)

	// Test 3: Multiple batches
	batch2 := &AggregatorBallotBatch{
		ProcessID: processID.Marshal(),
		Ballots: []*AggregatorBallot{
			{
				Nullifier:  new(big.Int).SetBytes(bytes.Repeat([]byte{2}, 32)),
				Address:    new(big.Int).SetBytes(bytes.Repeat([]byte{2}, 20)),
				Commitment: new(big.Int).SetBytes(bytes.Repeat([]byte{2}, 32)),
			},
		},
	}

	// Push batch2 and wait
	c.Assert(st.PushBallotBatch(batch2), qt.IsNil)

	// Get and verify batch2
	b2, b2key, err := st.NextBallotBatch(processID.Marshal())
	c.Assert(err, qt.IsNil)
	c.Assert(b2, qt.IsNotNil)
	c.Assert(len(b2.Ballots), qt.Equals, 1)
	c.Assert(b2.Ballots[0].Nullifier.Cmp(batch2.Ballots[0].Nullifier), qt.Equals, 0)

	// Mark batch2 done and wait
	c.Assert(st.MarkBallotBatchDone(b2key), qt.IsNil)

	// Push and verify batch3
	batch3 := &AggregatorBallotBatch{
		ProcessID: processID.Marshal(),
		Ballots: []*AggregatorBallot{
			{
				Nullifier:  new(big.Int).SetBytes(bytes.Repeat([]byte{3}, 32)),
				Address:    new(big.Int).SetBytes(bytes.Repeat([]byte{3}, 20)),
				Commitment: new(big.Int).SetBytes(bytes.Repeat([]byte{3}, 32)),
			},
		},
	}

	c.Assert(st.PushBallotBatch(batch3), qt.IsNil)

	b3, b3key, err := st.NextBallotBatch(processID.Marshal())
	c.Assert(err, qt.IsNil)
	c.Assert(b3, qt.IsNotNil)
	c.Assert(len(b3.Ballots), qt.Equals, 1)
	c.Assert(b3.Ballots[0].Nullifier.Cmp(batch3.Ballots[0].Nullifier), qt.Equals, 0)

	// Mark batch3 done
	c.Assert(st.MarkBallotBatchDone(b3key), qt.IsNil)

	// Verify no more batches
	_, _, err = st.NextBallotBatch(processID.Marshal())
	c.Assert(err, qt.Equals, ErrNoMoreElements)

	// Test 4: Different process ID
	anotherPID := types.ProcessID{
		Address: common.Address{},
		ChainID: 0,
		Nonce:   999,
	}
	_, _, err = st.NextBallotBatch(anotherPID.Marshal())
	c.Assert(err, qt.Equals, ErrNoMoreElements)
}
