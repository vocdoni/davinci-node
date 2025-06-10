package storage

import (
	"math/big"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/metadb"
)

// TestProcessStatsConcurrency tests that updates maintain consistency
// under concurrent operations.
func TestProcessStatsConcurrency(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create a process
	processID := &types.ProcessID{
		Address: common.Address{},
		Nonce:   42,
		ChainID: 1,
	}

	err = st.SetProcess(createTestProcess(processID))
	c.Assert(err, qt.IsNil)

	// Test concurrent ballot processing
	numGoroutines := 10
	ballotsPerGoroutine := 20
	wg := sync.WaitGroup{}

	// Start multiple goroutines that will process ballots concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			for j := 0; j < ballotsPerGoroutine; j++ {
				// Create a unique ballot
				ballot := &Ballot{
					ProcessID:        processID.Marshal(),
					Nullifier:        big.NewInt(int64(routineID*1000 + j)),
					Address:          big.NewInt(int64(routineID*10000 + j)),
					BallotInputsHash: big.NewInt(int64(routineID*100000 + j)),
				}

				// Push ballot (pending +1)
				err := st.PushBallot(ballot)
				if err != nil && err != ErroBallotAlreadyExists {
					panic(err)
				}

				// Get the ballot
				b, key, err := st.NextBallotForWorker()
				if err != nil {
					// Another goroutine might have taken it
					continue
				}

				// Mark it as done (pending -1, verified +1, currentBatch +1)
				verifiedBallot := &VerifiedBallot{
					ProcessID:   b.ProcessID,
					Nullifier:   b.Nullifier,
					VoteID:      b.VoteID(),
					VoterWeight: big.NewInt(1),
				}
				err = st.MarkBallotDone(key, verifiedBallot)
				if err != nil {
					panic(err)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Check the final stats
	finalProcess, err := st.Process(processID)
	c.Assert(err, qt.IsNil)

	stats := finalProcess.SequencerStats
	totalExpected := numGoroutines * ballotsPerGoroutine

	// All ballots should be processed, so pending should be 0
	c.Assert(stats.PendingVotesCount, qt.Equals, 0,
		qt.Commentf("Expected 0 pending votes, got %d", stats.PendingVotesCount))

	// All ballots should be verified
	c.Assert(stats.VerifiedVotesCount, qt.Equals, totalExpected,
		qt.Commentf("Expected %d verified votes, got %d", totalExpected, stats.VerifiedVotesCount))

	// Current batch size should equal verified count (since no aggregation happened)
	c.Assert(stats.CurrentBatchSize, qt.Equals, totalExpected,
		qt.Commentf("Expected current batch size %d, got %d", totalExpected, stats.CurrentBatchSize))
}

// TestProcessStatsAggregation tests that stats remain consistent during aggregation
func TestProcessStatsAggregation(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create a process
	processID := &types.ProcessID{
		Address: common.Address{},
		Nonce:   43,
		ChainID: 1,
	}

	err = st.SetProcess(createTestProcess(processID))
	c.Assert(err, qt.IsNil)

	// Create and process some ballots
	numBallots := 10
	for i := 0; i < numBallots; i++ {
		ballot := &Ballot{
			ProcessID:        processID.Marshal(),
			Nullifier:        big.NewInt(int64(i)),
			Address:          big.NewInt(int64(i + 1000)),
			BallotInputsHash: big.NewInt(int64(i + 2000)),
		}

		// Push ballot
		err := st.PushBallot(ballot)
		c.Assert(err, qt.IsNil)

		// Get and mark as done
		b, key, err := st.NextBallot()
		c.Assert(err, qt.IsNil)

		verifiedBallot := &VerifiedBallot{
			ProcessID:   b.ProcessID,
			Nullifier:   b.Nullifier,
			VoteID:      b.VoteID(),
			VoterWeight: big.NewInt(1),
		}
		err = st.MarkBallotDone(key, verifiedBallot)
		c.Assert(err, qt.IsNil)
	}

	// Check intermediate stats
	proc1, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(proc1.SequencerStats.PendingVotesCount, qt.Equals, 0)
	c.Assert(proc1.SequencerStats.VerifiedVotesCount, qt.Equals, numBallots)
	c.Assert(proc1.SequencerStats.CurrentBatchSize, qt.Equals, numBallots)

	// Simulate aggregation
	// Pull verified ballots
	verifiedBallots, keys, err := st.PullVerifiedBallots(processID.Marshal(), numBallots)
	c.Assert(err, qt.IsNil)
	c.Assert(len(verifiedBallots), qt.Equals, numBallots)

	// Create aggregator batch
	aggBallots := make([]*AggregatorBallot, len(verifiedBallots))
	for i, vb := range verifiedBallots {
		aggBallots[i] = &AggregatorBallot{
			VoteID:    vb.VoteID,
			Nullifier: vb.Nullifier,
			Address:   vb.Address,
		}
	}

	batch := &AggregatorBallotBatch{
		ProcessID: processID.Marshal(),
		Ballots:   aggBallots,
	}

	// Push the batch (this should update aggregated votes and current batch size)
	err = st.PushBallotBatch(batch)
	c.Assert(err, qt.IsNil)

	// Mark verified ballots as done
	err = st.MarkVerifiedBallotsDone(keys...)
	c.Assert(err, qt.IsNil)

	// Check final stats
	finalProc, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	stats := finalProc.SequencerStats

	c.Assert(stats.PendingVotesCount, qt.Equals, 0)
	c.Assert(stats.VerifiedVotesCount, qt.Equals, numBallots)
	c.Assert(stats.AggregatedVotesCount, qt.Equals, numBallots)
	c.Assert(stats.CurrentBatchSize, qt.Equals, 0) // Should be 0 after aggregation
	c.Assert(stats.LastBatchSize, qt.Equals, numBallots)

	// Verify the invariant: verified = aggregated + currentBatch
	c.Assert(stats.VerifiedVotesCount, qt.Equals, stats.AggregatedVotesCount+stats.CurrentBatchSize)
}

// TestProcessStatsRaceCondition specifically tests the scenario from the issue
func TestProcessStatsRaceCondition(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create a process
	processID := &types.ProcessID{
		Address: common.Address{},
		Nonce:   44,
		ChainID: 1,
	}

	err = st.SetProcess(createTestProcess(processID))
	c.Assert(err, qt.IsNil)

	// Simulate the race condition scenario
	// Multiple goroutines processing ballots and aggregating simultaneously
	wg := sync.WaitGroup{}
	numWorkers := 5
	ballotsPerWorker := 20

	// Worker goroutines that process individual ballots
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < ballotsPerWorker; j++ {
				ballot := &Ballot{
					ProcessID:        processID.Marshal(),
					Nullifier:        big.NewInt(int64(workerID*1000 + j)),
					Address:          big.NewInt(int64(workerID*10000 + j)),
					BallotInputsHash: big.NewInt(int64(workerID*100000 + j)),
				}

				// Push and process ballot
				if err := st.PushBallot(ballot); err != nil && err != ErroBallotAlreadyExists {
					panic(err)
				}

				b, key, err := st.NextBallotForWorker()
				if err == ErrNoMoreElements {
					continue
				}
				if err != nil {
					panic(err)
				}

				verifiedBallot := &VerifiedBallot{
					ProcessID:   b.ProcessID,
					Nullifier:   b.Nullifier,
					VoteID:      b.VoteID(),
					VoterWeight: big.NewInt(1),
				}
				if err := st.MarkBallotDone(key, verifiedBallot); err != nil {
					panic(err)
				}
			}
		}(i)
	}

	// Aggregator goroutine that periodically aggregates ballots
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ { // Try to aggregate 5 times
			time.Sleep(10 * time.Millisecond) // Small delay

			// Try to pull and aggregate verified ballots
			verifiedBallots, keys, err := st.PullVerifiedBallots(processID.Marshal(), 10)
			if err == ErrNotFound {
				continue
			}
			if err != nil {
				panic(err)
			}

			if len(verifiedBallots) > 0 {
				aggBallots := make([]*AggregatorBallot, len(verifiedBallots))
				for j, vb := range verifiedBallots {
					aggBallots[j] = &AggregatorBallot{
						VoteID:    vb.VoteID,
						Nullifier: vb.Nullifier,
						Address:   vb.Address,
					}
				}

				batch := &AggregatorBallotBatch{
					ProcessID: processID.Marshal(),
					Ballots:   aggBallots,
				}

				if err := st.PushBallotBatch(batch); err != nil {
					panic(err)
				}

				if err := st.MarkVerifiedBallotsDone(keys...); err != nil {
					panic(err)
				}
			}
		}
	}()

	// Wait for all operations to complete
	wg.Wait()

	// Allow time for any remaining operations
	time.Sleep(100 * time.Millisecond)

	// Check final stats consistency
	finalProc, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	stats := finalProc.SequencerStats

	totalExpected := numWorkers * ballotsPerWorker

	// All ballots should be accounted for
	totalProcessed := stats.AggregatedVotesCount + stats.CurrentBatchSize
	c.Assert(totalProcessed, qt.Equals, stats.VerifiedVotesCount,
		qt.Commentf("Mismatch: aggregated (%d) + currentBatch (%d) != verified (%d)",
			stats.AggregatedVotesCount, stats.CurrentBatchSize, stats.VerifiedVotesCount))

	// Pending should be 0
	c.Assert(stats.PendingVotesCount, qt.Equals, 0,
		qt.Commentf("Expected 0 pending votes, got %d", stats.PendingVotesCount))

	// Total verified should match what we created
	c.Assert(stats.VerifiedVotesCount, qt.Equals, totalExpected,
		qt.Commentf("Expected %d verified votes, got %d", totalExpected, stats.VerifiedVotesCount))

	t.Logf("Final stats: pending=%d, verified=%d, aggregated=%d, currentBatch=%d",
		stats.PendingVotesCount, stats.VerifiedVotesCount,
		stats.AggregatedVotesCount, stats.CurrentBatchSize)
}

// TestGetTotalPendingBallots tests that GetTotalPendingBallots correctly
// sums up pending ballots from all processes using stats.
func TestGetTotalPendingBallots(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create multiple processes
	processID1 := &types.ProcessID{
		Address: common.Address{1},
		Nonce:   1,
		ChainID: 1,
	}

	processID2 := &types.ProcessID{
		Address: common.Address{2},
		Nonce:   2,
		ChainID: 1,
	}

	processID3 := &types.ProcessID{
		Address: common.Address{3},
		Nonce:   3,
		ChainID: 1,
	}

	// Create processes with different pending ballot counts
	processes := []*types.Process{
		{
			ID:               processID1.Marshal(),
			Status:           0,
			StartTime:        time.Now(),
			Duration:         time.Hour,
			MetadataURI:      "http://example.com/metadata",
			StateRoot:        new(types.BigInt).SetUint64(100),
			SequencerStats:   types.SequencerProcessStats{PendingVotesCount: 5},
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
		},
		{
			ID:               processID2.Marshal(),
			Status:           0,
			StartTime:        time.Now(),
			Duration:         time.Hour,
			MetadataURI:      "http://example.com/metadata",
			StateRoot:        new(types.BigInt).SetUint64(100),
			SequencerStats:   types.SequencerProcessStats{PendingVotesCount: 3},
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
		},
		{
			ID:               processID3.Marshal(),
			Status:           0,
			StartTime:        time.Now(),
			Duration:         time.Hour,
			MetadataURI:      "http://example.com/metadata",
			StateRoot:        new(types.BigInt).SetUint64(100),
			SequencerStats:   types.SequencerProcessStats{PendingVotesCount: 7},
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
		},
	}

	// Store all processes
	for _, process := range processes {
		err = st.SetProcess(process)
		c.Assert(err, qt.IsNil)
	}

	// Test: GetTotalPendingBallots should return sum of all pending votes
	total, err := st.TotalPendingBallots()
	c.Assert(err, qt.IsNil)
	c.Assert(total, qt.Equals, 15, qt.Commentf("Expected total of 5+3+7=15 pending ballots"))

	// Test: Compare with CountPendingBallots (should be 0 since no actual ballots in queue)
	actualCount := st.CountPendingBallots()
	c.Assert(actualCount, qt.Equals, 0, qt.Commentf("No actual ballots in queue"))

	// Test: Add some actual ballots and verify stats are updated
	ballot1 := &Ballot{
		ProcessID:        processID1.Marshal(),
		Nullifier:        big.NewInt(1),
		Address:          big.NewInt(1000),
		BallotInputsHash: big.NewInt(2000),
	}
	err = st.PushBallot(ballot1)
	c.Assert(err, qt.IsNil)

	// GetTotalPendingBallots should now return 16 (15 + 1)
	total, err = st.TotalPendingBallots()
	c.Assert(err, qt.IsNil)
	c.Assert(total, qt.Equals, 16, qt.Commentf("Expected total of 16 pending ballots after adding one"))

	// CountPendingBallots should return 1
	actualCount = st.CountPendingBallots()
	c.Assert(actualCount, qt.Equals, 1, qt.Commentf("Should have 1 actual ballot in queue"))

	// Test: Process the ballot and verify stats are updated
	b, key, err := st.NextBallot()
	c.Assert(err, qt.IsNil)
	c.Assert(b, qt.IsNotNil)

	verifiedBallot := &VerifiedBallot{
		ProcessID:   b.ProcessID,
		Nullifier:   b.Nullifier,
		VoteID:      b.VoteID(),
		VoterWeight: big.NewInt(1),
	}
	err = st.MarkBallotDone(key, verifiedBallot)
	c.Assert(err, qt.IsNil)

	// GetTotalPendingBallots should return 15 again (16 - 1)
	total, err = st.TotalPendingBallots()
	c.Assert(err, qt.IsNil)
	c.Assert(total, qt.Equals, 15, qt.Commentf("Expected total of 15 pending ballots after processing one"))
}

// TestMarkVerifiedBallotsFailed tests that marking verified ballots as failed
// properly updates the counters to prevent mismatches.
func TestMarkVerifiedBallotsFailed(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create a process
	processID := &types.ProcessID{
		Address: common.Address{},
		Nonce:   100,
		ChainID: 1,
	}

	err = st.SetProcess(createTestProcess(processID))
	c.Assert(err, qt.IsNil)

	// Create and process some ballots to verified state
	numBallots := 5
	var keys [][]byte
	for i := 0; i < numBallots; i++ {
		ballot := &Ballot{
			ProcessID:        processID.Marshal(),
			Nullifier:        big.NewInt(int64(i + 1000)),
			Address:          big.NewInt(int64(i + 2000)),
			BallotInputsHash: big.NewInt(int64(i + 3000)),
		}

		// Push ballot
		err := st.PushBallot(ballot)
		c.Assert(err, qt.IsNil)

		// Get and mark as done (verified)
		b, key, err := st.NextBallot()
		c.Assert(err, qt.IsNil)

		verifiedBallot := &VerifiedBallot{
			ProcessID:   b.ProcessID,
			Nullifier:   b.Nullifier,
			VoteID:      b.VoteID(),
			VoterWeight: big.NewInt(1),
		}
		err = st.MarkBallotDone(key, verifiedBallot)
		c.Assert(err, qt.IsNil)

		// Store the verified ballot key for later failure
		combKey := append(processID.Marshal(), key...)
		keys = append(keys, combKey)
	}

	// Check stats before failure
	proc1, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(proc1.SequencerStats.PendingVotesCount, qt.Equals, 0)
	c.Assert(proc1.SequencerStats.VerifiedVotesCount, qt.Equals, numBallots)
	c.Assert(proc1.SequencerStats.CurrentBatchSize, qt.Equals, numBallots)

	// Mark 3 verified ballots as failed
	failedCount := 3
	failedKeys := keys[:failedCount]
	err = st.MarkVerifiedBallotsFailed(failedKeys...)
	c.Assert(err, qt.IsNil)

	// Check stats after failure
	proc2, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	stats := proc2.SequencerStats

	expectedVerified := numBallots - failedCount
	expectedCurrentBatch := numBallots - failedCount

	c.Assert(stats.PendingVotesCount, qt.Equals, 0)
	c.Assert(stats.VerifiedVotesCount, qt.Equals, expectedVerified,
		qt.Commentf("Expected %d verified votes after failure, got %d", expectedVerified, stats.VerifiedVotesCount))
	c.Assert(stats.CurrentBatchSize, qt.Equals, expectedCurrentBatch,
		qt.Commentf("Expected current batch size %d after failure, got %d", expectedCurrentBatch, stats.CurrentBatchSize))

	t.Logf("After marking %d ballots failed: pending=%d, verified=%d, currentBatch=%d",
		failedCount, stats.PendingVotesCount, stats.VerifiedVotesCount, stats.CurrentBatchSize)
}

// TestMarkBallotBatchFailed tests that marking an aggregated batch as failed
// properly reverts the aggregation counters.
func TestMarkBallotBatchFailed(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create a process
	processID := &types.ProcessID{
		Address: common.Address{},
		Nonce:   101,
		ChainID: 1,
	}

	err = st.SetProcess(createTestProcess(processID))
	c.Assert(err, qt.IsNil)

	// Create and process some ballots to verified state
	numBallots := 8
	for i := 0; i < numBallots; i++ {
		ballot := &Ballot{
			ProcessID:        processID.Marshal(),
			Nullifier:        big.NewInt(int64(i + 1000)),
			Address:          big.NewInt(int64(i + 2000)),
			BallotInputsHash: big.NewInt(int64(i + 3000)),
		}

		// Push ballot
		err := st.PushBallot(ballot)
		c.Assert(err, qt.IsNil)

		// Get and mark as done (verified)
		b, key, err := st.NextBallot()
		c.Assert(err, qt.IsNil)

		verifiedBallot := &VerifiedBallot{
			ProcessID:   b.ProcessID,
			Nullifier:   b.Nullifier,
			VoteID:      b.VoteID(),
			VoterWeight: big.NewInt(1),
		}
		err = st.MarkBallotDone(key, verifiedBallot)
		c.Assert(err, qt.IsNil)
	}

	// Pull verified ballots and create aggregator batch
	verifiedBallots, keys, err := st.PullVerifiedBallots(processID.Marshal(), numBallots)
	c.Assert(err, qt.IsNil)
	c.Assert(len(verifiedBallots), qt.Equals, numBallots)

	// Create aggregator batch
	aggBallots := make([]*AggregatorBallot, len(verifiedBallots))
	for i, vb := range verifiedBallots {
		aggBallots[i] = &AggregatorBallot{
			VoteID:    vb.VoteID,
			Nullifier: vb.Nullifier,
			Address:   vb.Address,
		}
	}

	batch := &AggregatorBallotBatch{
		ProcessID: processID.Marshal(),
		Ballots:   aggBallots,
	}

	// Push the batch (this updates aggregated votes and decreases current batch size)
	err = st.PushBallotBatch(batch)
	c.Assert(err, qt.IsNil)

	// Mark verified ballots as done
	err = st.MarkVerifiedBallotsDone(keys...)
	c.Assert(err, qt.IsNil)

	// Check stats after aggregation
	proc1, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(proc1.SequencerStats.VerifiedVotesCount, qt.Equals, numBallots)
	c.Assert(proc1.SequencerStats.AggregatedVotesCount, qt.Equals, numBallots)
	c.Assert(proc1.SequencerStats.CurrentBatchSize, qt.Equals, 0)

	// Get the batch key to mark it as failed
	batchEntry, batchKey, err := st.NextBallotBatch(processID.Marshal())
	c.Assert(err, qt.IsNil)
	c.Assert(batchEntry, qt.IsNotNil)
	c.Assert(len(batchEntry.Ballots), qt.Equals, numBallots)

	// Mark the batch as failed
	err = st.MarkBallotBatchFailed(batchKey)
	c.Assert(err, qt.IsNil)

	// Check stats after batch failure
	proc2, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	stats := proc2.SequencerStats

	// Aggregated votes should be reverted, current batch size should be restored
	c.Assert(stats.VerifiedVotesCount, qt.Equals, numBallots,
		qt.Commentf("VerifiedVotesCount should remain %d", numBallots))
	c.Assert(stats.AggregatedVotesCount, qt.Equals, 0,
		qt.Commentf("AggregatedVotesCount should be reverted to 0, got %d", stats.AggregatedVotesCount))
	c.Assert(stats.CurrentBatchSize, qt.Equals, numBallots,
		qt.Commentf("CurrentBatchSize should be restored to %d, got %d", numBallots, stats.CurrentBatchSize))

	t.Logf("After batch failure: verified=%d, aggregated=%d, currentBatch=%d",
		stats.VerifiedVotesCount, stats.AggregatedVotesCount, stats.CurrentBatchSize)
}

// TestProcessStatsNegativeValuePrevention tests that the safeguards prevent
// negative values in process stats counters.
func TestProcessStatsNegativeValuePrevention(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create a process
	processID := &types.ProcessID{
		Address: common.Address{},
		Nonce:   102,
		ChainID: 1,
	}

	err = st.SetProcess(createTestProcess(processID))
	c.Assert(err, qt.IsNil)

	// Test attempting to set negative values
	updates := []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsPendingVotes, Delta: -10},   // Should be clamped to 0
		{TypeStats: types.TypeStatsVerifiedVotes, Delta: -5},   // Should be clamped to 0
		{TypeStats: types.TypeStatsAggregatedVotes, Delta: -3}, // Should be clamped to 0
	}

	err = st.updateProcessStats(processID.Marshal(), updates)
	c.Assert(err, qt.IsNil)

	// Check that all values are 0 (clamped)
	proc, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	stats := proc.SequencerStats

	c.Assert(stats.PendingVotesCount, qt.Equals, 0)
	c.Assert(stats.VerifiedVotesCount, qt.Equals, 0)
	c.Assert(stats.AggregatedVotesCount, qt.Equals, 0)

	// Test with positive values first, then negative deltas
	updates1 := []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsPendingVotes, Delta: 5},
		{TypeStats: types.TypeStatsVerifiedVotes, Delta: 3},
		{TypeStats: types.TypeStatsAggregatedVotes, Delta: 2},
	}

	err = st.updateProcessStats(processID.Marshal(), updates1)
	c.Assert(err, qt.IsNil)

	// Now apply negative deltas that would make some values negative
	updates2 := []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsPendingVotes, Delta: -10},   // 5 - 10 = -5, should be clamped to 0
		{TypeStats: types.TypeStatsVerifiedVotes, Delta: -2},   // 3 - 2 = 1, should remain 1
		{TypeStats: types.TypeStatsAggregatedVotes, Delta: -5}, // 2 - 5 = -3, should be clamped to 0
	}

	err = st.updateProcessStats(processID.Marshal(), updates2)
	c.Assert(err, qt.IsNil)

	// Check final values
	proc2, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	finalStats := proc2.SequencerStats

	c.Assert(finalStats.PendingVotesCount, qt.Equals, 0, qt.Commentf("Should be clamped to 0"))
	c.Assert(finalStats.VerifiedVotesCount, qt.Equals, 1, qt.Commentf("Should be 1"))
	c.Assert(finalStats.AggregatedVotesCount, qt.Equals, 0, qt.Commentf("Should be clamped to 0"))

	// Test that CurrentBatchSize cannot be negative (should be clamped to 0)
	updates3 := []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsCurrentBatchSize, Delta: -10}, // Should be clamped to 0
	}

	err = st.updateProcessStats(processID.Marshal(), updates3)
	c.Assert(err, qt.IsNil)

	proc3, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(proc3.SequencerStats.CurrentBatchSize, qt.Equals, 0, qt.Commentf("CurrentBatchSize should be clamped to 0 when negative"))

	// Test that LastBatchSize cannot be negative
	updates4 := []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsLastBatchSize, Delta: -5}, // Should be clamped to 0
	}

	err = st.updateProcessStats(processID.Marshal(), updates4)
	c.Assert(err, qt.IsNil)

	proc4, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(proc4.SequencerStats.LastBatchSize, qt.Equals, 0, qt.Commentf("LastBatchSize should be clamped to 0 when negative"))

	t.Logf("Final stats with safeguards: pending=%d, verified=%d, aggregated=%d, currentBatch=%d, lastBatch=%d",
		finalStats.PendingVotesCount, finalStats.VerifiedVotesCount,
		finalStats.AggregatedVotesCount, proc3.SequencerStats.CurrentBatchSize, proc4.SequencerStats.LastBatchSize)
}
