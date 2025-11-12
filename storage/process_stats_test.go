package storage

import (
	"fmt"
	"math/big"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/types"
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
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	err = st.NewProcess(createTestProcess(processID))
	c.Assert(err, qt.IsNil)

	// Test concurrent ballot processing
	numGoroutines := 10
	ballotsPerGoroutine := 20
	wg := sync.WaitGroup{}

	// Start multiple goroutines that will process ballots concurrently
	for i := range numGoroutines {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			for j := 0; j < ballotsPerGoroutine; j++ {
				// Create a unique ballot
				ballot := &Ballot{
					ProcessID: processID.Marshal(),
					Address:   big.NewInt(int64(routineID*10000 + j)),
					VoteID:    fmt.Appendf(nil, "vote%d", routineID*100+j),
				}

				// Push ballot (pending +1)
				err := st.PushPendingBallot(ballot)
				if err != nil && err != ErroBallotAlreadyExists {
					panic(err)
				}

				// Get the ballot
				b, key, err := st.NextPendingBallot()
				if err != nil {
					// Another goroutine might have taken it
					continue
				}

				// Mark it as done (pending -1, verified +1, currentBatch +1)
				verifiedBallot := &VerifiedBallot{
					ProcessID:   b.ProcessID,
					VoteID:      b.VoteID,
					VoterWeight: big.NewInt(1),
				}
				err = st.MarkBallotVerified(key, verifiedBallot)
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
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	err = st.NewProcess(createTestProcess(processID))
	c.Assert(err, qt.IsNil)

	// Create and process some ballots
	numBallots := 10
	for i := range numBallots {
		ballot := &Ballot{
			ProcessID: processID.Marshal(),
			Address:   big.NewInt(int64(i + 1000)),
			VoteID:    fmt.Appendf(nil, "vote%d", i),
		}

		// Push ballot
		err := st.PushPendingBallot(ballot)
		c.Assert(err, qt.IsNil)

		// Get and mark as done
		b, key, err := st.NextPendingBallot()
		c.Assert(err, qt.IsNil)

		verifiedBallot := &VerifiedBallot{
			ProcessID:   b.ProcessID,
			Address:     b.Address,
			VoteID:      b.VoteID,
			VoterWeight: big.NewInt(1),
		}
		err = st.MarkBallotVerified(key, verifiedBallot)
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
			VoteID:  vb.VoteID,
			Address: vb.Address,
		}
	}

	batch := &AggregatorBallotBatch{
		ProcessID: processID.Marshal(),
		Ballots:   aggBallots,
	}

	// Push the batch (this should update aggregated votes and current batch size)
	err = st.PushAggregatorBatch(batch)
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
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	err = st.NewProcess(createTestProcess(processID))
	c.Assert(err, qt.IsNil)

	// Simulate the race condition scenario
	// Multiple goroutines processing ballots and aggregating simultaneously
	wg := sync.WaitGroup{}
	numWorkers := 5
	ballotsPerWorker := 20

	// Worker goroutines that process individual ballots
	for i := range numWorkers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < ballotsPerWorker; j++ {
				ballot := &Ballot{
					ProcessID: processID.Marshal(),
					Address:   big.NewInt(int64(workerID*10000 + j)),
					VoteID:    fmt.Appendf(nil, "vote%d-%d", workerID, j),
				}

				// Push and process ballot
				if err := st.PushPendingBallot(ballot); err != nil && err != ErroBallotAlreadyExists {
					panic(err)
				}

				b, key, err := st.NextPendingBallot()
				if err == ErrNoMoreElements {
					continue
				}
				if err != nil {
					panic(err)
				}

				verifiedBallot := &VerifiedBallot{
					ProcessID:   b.ProcessID,
					VoteID:      b.VoteID,
					VoterWeight: big.NewInt(1),
				}
				if err := st.MarkBallotVerified(key, verifiedBallot); err != nil {
					panic(err)
				}
			}
		}(i)
	}

	// Aggregator goroutine that periodically aggregates ballots
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 5 { // Try to aggregate 5 times
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
						VoteID:  vb.VoteID,
						Address: vb.Address,
					}
				}

				batch := &AggregatorBallotBatch{
					ProcessID: processID.Marshal(),
					Ballots:   aggBallots,
				}

				if err := st.PushAggregatorBatch(batch); err != nil {
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
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	processID2 := &types.ProcessID{
		Address: common.Address{2},
		Nonce:   2,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	processID3 := &types.ProcessID{
		Address: common.Address{3},
		Nonce:   3,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	// Create processes with different pending ballot counts
	processes := []*types.Process{
		{
			ID:             processID1.Marshal(),
			Status:         0,
			StartTime:      time.Now(),
			Duration:       time.Hour,
			MetadataURI:    "http://example.com/metadata",
			StateRoot:      new(types.BigInt).SetUint64(100),
			SequencerStats: types.SequencerProcessStats{PendingVotesCount: 5},
			BallotMode: &types.BallotMode{
				NumFields:   8,
				MaxValue:    new(types.BigInt).SetUint64(100),
				MinValue:    new(types.BigInt).SetUint64(0),
				MaxValueSum: new(types.BigInt).SetUint64(0),
				MinValueSum: new(types.BigInt).SetUint64(0),
			},
			Census: &types.Census{
				CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
				CensusRoot:   make([]byte, 32),
				CensusURI:    "http://example.com/census",
			},
		},
		{
			ID:             processID2.Marshal(),
			Status:         0,
			StartTime:      time.Now(),
			Duration:       time.Hour,
			MetadataURI:    "http://example.com/metadata",
			StateRoot:      new(types.BigInt).SetUint64(100),
			SequencerStats: types.SequencerProcessStats{PendingVotesCount: 3},
			BallotMode: &types.BallotMode{
				NumFields:   8,
				MaxValue:    new(types.BigInt).SetUint64(100),
				MinValue:    new(types.BigInt).SetUint64(0),
				MaxValueSum: new(types.BigInt).SetUint64(0),
				MinValueSum: new(types.BigInt).SetUint64(0),
			},
			Census: &types.Census{
				CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
				CensusRoot:   make([]byte, 32),
				CensusURI:    "http://example.com/census",
			},
		},
		{
			ID:             processID3.Marshal(),
			Status:         0,
			StartTime:      time.Now(),
			Duration:       time.Hour,
			MetadataURI:    "http://example.com/metadata",
			StateRoot:      new(types.BigInt).SetUint64(100),
			SequencerStats: types.SequencerProcessStats{PendingVotesCount: 7},
			BallotMode: &types.BallotMode{
				NumFields:   8,
				MaxValue:    new(types.BigInt).SetUint64(100),
				MinValue:    new(types.BigInt).SetUint64(0),
				MaxValueSum: new(types.BigInt).SetUint64(0),
				MinValueSum: new(types.BigInt).SetUint64(0),
			},
			Census: &types.Census{
				CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
				CensusRoot:   make([]byte, 32),
				CensusURI:    "http://example.com/census",
			},
		},
	}

	// Store all processes
	for _, process := range processes {
		err = st.NewProcess(process)
		c.Assert(err, qt.IsNil)
	}

	// Test: TotalPendingBallots should return 0 initially (since no actual ballots were pushed)
	// The pre-set PendingVotesCount in processes are not tracked by the new total system
	total := st.TotalPendingBallots()
	c.Assert(total, qt.Equals, 0, qt.Commentf("Expected 0 as no actual ballots were pushed through the system"))

	// Test: Compare with CountPendingBallots (should be 0 since no actual ballots in queue)
	actualCount := st.CountPendingBallots()
	c.Assert(actualCount, qt.Equals, 0, qt.Commentf("No actual ballots in queue"))

	// Test: Add some actual ballots and verify stats are updated
	ballot1 := &Ballot{
		ProcessID:        processID1.Marshal(),
		Address:          big.NewInt(1000),
		BallotInputsHash: big.NewInt(2000),
	}
	err = st.PushPendingBallot(ballot1)
	c.Assert(err, qt.IsNil)

	// TotalPendingBallots should now return 1 (0 + 1)
	total = st.TotalPendingBallots()
	c.Assert(total, qt.Equals, 1, qt.Commentf("Expected total of 1 pending ballot after adding one"))

	// CountPendingBallots should return 1
	actualCount = st.CountPendingBallots()
	c.Assert(actualCount, qt.Equals, 1, qt.Commentf("Should have 1 actual ballot in queue"))

	// Test: Process the ballot and verify stats are updated
	b, key, err := st.NextPendingBallot()
	c.Assert(err, qt.IsNil)
	c.Assert(b, qt.IsNotNil)

	verifiedBallot := &VerifiedBallot{
		ProcessID:   b.ProcessID,
		VoteID:      b.VoteID,
		VoterWeight: big.NewInt(1),
	}
	err = st.MarkBallotVerified(key, verifiedBallot)
	c.Assert(err, qt.IsNil)

	// TotalPendingBallots should return 0 again (1 - 1)
	total = st.TotalPendingBallots()
	c.Assert(total, qt.Equals, 0, qt.Commentf("Expected total of 0 pending ballots after processing one"))

	// Test: Verify individual process stats were also updated correctly
	proc1, err := st.Process(processID1)
	c.Assert(err, qt.IsNil)
	c.Assert(proc1.SequencerStats.PendingVotesCount, qt.Equals, 5, qt.Commentf("Process1 should still have its original pending count of 5"))
	c.Assert(proc1.SequencerStats.VerifiedVotesCount, qt.Equals, 1, qt.Commentf("Process1 should have 1 verified vote"))
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
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	err = st.NewProcess(createTestProcess(processID))
	c.Assert(err, qt.IsNil)

	// Create and process some ballots to verified state
	numBallots := 5
	var keys [][]byte
	for i := range numBallots {
		ballot := &Ballot{
			ProcessID: processID.Marshal(),
			Address:   big.NewInt(int64(i + 2000)),
			VoteID:    fmt.Appendf(nil, "vote%d", i),
		}

		// Push ballot
		err := st.PushPendingBallot(ballot)
		c.Assert(err, qt.IsNil)

		// Get and mark as done (verified)
		b, key, err := st.NextPendingBallot()
		c.Assert(err, qt.IsNil)

		verifiedBallot := &VerifiedBallot{
			ProcessID:   b.ProcessID,
			Address:     b.Address,
			VoteID:      b.VoteID,
			VoterWeight: big.NewInt(1),
		}
		err = st.MarkBallotVerified(key, verifiedBallot)
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
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	err = st.NewProcess(createTestProcess(processID))
	c.Assert(err, qt.IsNil)

	// Create and process some ballots to verified state
	numBallots := 8
	for i := range numBallots {
		ballot := &Ballot{
			ProcessID: processID.Marshal(),
			Address:   big.NewInt(int64(i + 2000)),
			VoteID:    fmt.Appendf(nil, "vote%d", i),
		}

		// Push ballot
		err := st.PushPendingBallot(ballot)
		c.Assert(err, qt.IsNil)

		// Get and mark as done (verified)
		b, key, err := st.NextPendingBallot()
		c.Assert(err, qt.IsNil)

		verifiedBallot := &VerifiedBallot{
			ProcessID:   b.ProcessID,
			Address:     b.Address,
			VoteID:      b.VoteID,
			VoterWeight: big.NewInt(1),
		}
		err = st.MarkBallotVerified(key, verifiedBallot)
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
			VoteID:  vb.VoteID,
			Address: vb.Address,
		}
	}

	batch := &AggregatorBallotBatch{
		ProcessID: processID.Marshal(),
		Ballots:   aggBallots,
	}

	// Push the batch (this updates aggregated votes and decreases current batch size)
	err = st.PushAggregatorBatch(batch)
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
	batchEntry, batchKey, err := st.NextAggregatorBatch(processID.Marshal())
	c.Assert(err, qt.IsNil)
	c.Assert(batchEntry, qt.IsNotNil)
	c.Assert(len(batchEntry.Ballots), qt.Equals, numBallots)

	// Mark the batch as failed
	err = st.MarkAggregatorBatchFailed(batchKey)
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
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	err = st.NewProcess(createTestProcess(processID))
	c.Assert(err, qt.IsNil)

	// Test attempting to set negative values (without clamping for some stats)
	updates := []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsPendingVotes, Delta: -10},           // Should be clamped to 0
		{TypeStats: types.TypeStatsVerifiedVotes, Delta: -5},           // No clamping, should be -5
		{TypeStats: types.TypeStatsAggregatedVotes, Delta: -3},         // No clamping, should be -3
		{TypeStats: types.TypeStatsStateTransitions, Delta: -2},        // No clamping, should be -2
		{TypeStats: types.TypeStatsSettledStateTransitions, Delta: -1}, // No clamping, should be -1
	}

	err = st.updateProcessStats(processID.Marshal(), updates)
	c.Assert(err, qt.IsNil)

	// Check values - pending should be clamped, others should be negative
	proc, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	stats := proc.SequencerStats

	c.Assert(stats.PendingVotesCount, qt.Equals, 0, qt.Commentf("PendingVotesCount should be clamped to 0"))
	c.Assert(stats.VerifiedVotesCount, qt.Equals, -5, qt.Commentf("VerifiedVotesCount should be -5 (no clamping)"))
	c.Assert(stats.AggregatedVotesCount, qt.Equals, -3, qt.Commentf("AggregatedVotesCount should be -3 (no clamping)"))
	c.Assert(stats.StateTransitionCount, qt.Equals, -2, qt.Commentf("StateTransitionCount should be -2 (no clamping)"))
	c.Assert(stats.SettledStateTransitionCount, qt.Equals, -1, qt.Commentf("SettledStateTransitionCount should be -1 (no clamping)"))

	// Test with positive values first, then negative deltas
	updates1 := []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsPendingVotes, Delta: 5},
		{TypeStats: types.TypeStatsVerifiedVotes, Delta: 8},    // 8 + (-5) = 3
		{TypeStats: types.TypeStatsAggregatedVotes, Delta: 5},  // 5 + (-3) = 2
		{TypeStats: types.TypeStatsStateTransitions, Delta: 3}, // 3 + (-2) = 1
	}

	err = st.updateProcessStats(processID.Marshal(), updates1)
	c.Assert(err, qt.IsNil)

	// Now apply negative deltas
	updates2 := []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsPendingVotes, Delta: -10},    // 5 - 10 = -5, should be clamped to 0
		{TypeStats: types.TypeStatsVerifiedVotes, Delta: -2},    // 3 - 2 = 1
		{TypeStats: types.TypeStatsAggregatedVotes, Delta: -5},  // 2 - 5 = -3 (no clamping)
		{TypeStats: types.TypeStatsStateTransitions, Delta: -2}, // 1 - 2 = -1 (no clamping)
	}

	err = st.updateProcessStats(processID.Marshal(), updates2)
	c.Assert(err, qt.IsNil)

	// Check final values
	proc2, err := st.Process(processID)
	c.Assert(err, qt.IsNil)
	finalStats := proc2.SequencerStats

	c.Assert(finalStats.PendingVotesCount, qt.Equals, 0, qt.Commentf("Should be clamped to 0"))
	c.Assert(finalStats.VerifiedVotesCount, qt.Equals, 1, qt.Commentf("Should be 1"))
	c.Assert(finalStats.AggregatedVotesCount, qt.Equals, -3, qt.Commentf("Should be -3 (no clamping)"))
	c.Assert(finalStats.StateTransitionCount, qt.Equals, -1, qt.Commentf("Should be -1 (no clamping)"))

	// Test that CurrentBatchSize can be negative (should be clamped to 0)
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

	t.Logf("Final stats with safeguards: pending=%d, verified=%d, aggregated=%d, currentBatch=%d, lastBatch=%d, stateTransitions=%d",
		finalStats.PendingVotesCount, finalStats.VerifiedVotesCount,
		finalStats.AggregatedVotesCount, proc3.SequencerStats.CurrentBatchSize,
		proc4.SequencerStats.LastBatchSize, finalStats.StateTransitionCount)
}

// TestTotalStats tests the TotalStats functionality
func TestTotalStats(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Test 1: TotalStats with no processes should return empty stats
	totalStats, err := st.TotalStats()
	c.Assert(err, qt.IsNil)
	c.Assert(totalStats.StateTransitionCount, qt.Equals, 0)
	c.Assert(totalStats.SettledStateTransitionCount, qt.Equals, 0)
	c.Assert(totalStats.AggregatedVotesCount, qt.Equals, 0)
	c.Assert(totalStats.VerifiedVotesCount, qt.Equals, 0)

	// Test 2: Create multiple processes and update their stats
	process1 := &types.ProcessID{
		Address: common.Address{1},
		Nonce:   1,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}
	process2 := &types.ProcessID{
		Address: common.Address{2},
		Nonce:   2,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}
	process3 := &types.ProcessID{
		Address: common.Address{3},
		Nonce:   3,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	// Create processes
	err = st.NewProcess(createTestProcess(process1))
	c.Assert(err, qt.IsNil)
	err = st.NewProcess(createTestProcess(process2))
	c.Assert(err, qt.IsNil)
	err = st.NewProcess(createTestProcess(process3))
	c.Assert(err, qt.IsNil)

	// Update stats for process1
	updates1 := []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsStateTransitions, Delta: 2},
		{TypeStats: types.TypeStatsSettledStateTransitions, Delta: 1},
		{TypeStats: types.TypeStatsAggregatedVotes, Delta: 10},
		{TypeStats: types.TypeStatsVerifiedVotes, Delta: 15},
	}
	err = st.updateProcessStats(process1.Marshal(), updates1)
	c.Assert(err, qt.IsNil)

	// Update stats for process2
	updates2 := []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsStateTransitions, Delta: 3},
		{TypeStats: types.TypeStatsSettledStateTransitions, Delta: 2},
		{TypeStats: types.TypeStatsAggregatedVotes, Delta: 20},
		{TypeStats: types.TypeStatsVerifiedVotes, Delta: 25},
	}
	err = st.updateProcessStats(process2.Marshal(), updates2)
	c.Assert(err, qt.IsNil)

	// Update stats for process3
	updates3 := []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsStateTransitions, Delta: 5},
		{TypeStats: types.TypeStatsSettledStateTransitions, Delta: 4},
		{TypeStats: types.TypeStatsAggregatedVotes, Delta: 30},
		{TypeStats: types.TypeStatsVerifiedVotes, Delta: 35},
	}
	err = st.updateProcessStats(process3.Marshal(), updates3)
	c.Assert(err, qt.IsNil)

	// Test 3: Verify total stats are correctly aggregated
	totalStats, err = st.TotalStats()
	c.Assert(err, qt.IsNil)
	c.Assert(totalStats.StateTransitionCount, qt.Equals, 10, qt.Commentf("Expected 2+3+5=10"))
	c.Assert(totalStats.SettledStateTransitionCount, qt.Equals, 7, qt.Commentf("Expected 1+2+4=7"))
	c.Assert(totalStats.AggregatedVotesCount, qt.Equals, 60, qt.Commentf("Expected 10+20+30=60"))
	c.Assert(totalStats.VerifiedVotesCount, qt.Equals, 75, qt.Commentf("Expected 15+25+35=75"))

	// Test 4: Update existing process and verify totals are updated
	updatesMore := []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsStateTransitions, Delta: 1},
		{TypeStats: types.TypeStatsVerifiedVotes, Delta: 5},
	}
	err = st.updateProcessStats(process1.Marshal(), updatesMore)
	c.Assert(err, qt.IsNil)

	totalStats, err = st.TotalStats()
	c.Assert(err, qt.IsNil)
	c.Assert(totalStats.StateTransitionCount, qt.Equals, 11, qt.Commentf("Expected 10+1=11"))
	c.Assert(totalStats.VerifiedVotesCount, qt.Equals, 80, qt.Commentf("Expected 75+5=80"))

	t.Logf("Final total stats: stateTransitions=%d, settledTransitions=%d, aggregated=%d, verified=%d",
		totalStats.StateTransitionCount, totalStats.SettledStateTransitionCount,
		totalStats.AggregatedVotesCount, totalStats.VerifiedVotesCount)
}

// TestTotalPendingBallotsNewFunctionality tests the new TotalPendingBallots method
func TestTotalPendingBallotsNewFunctionality(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Test 1: TotalPendingBallots with no processes should return 0
	total := st.TotalPendingBallots()
	c.Assert(total, qt.Equals, 0)

	// Test 2: Create multiple processes and track pending ballots
	process1 := &types.ProcessID{
		Address: common.Address{1},
		Nonce:   1,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}
	process2 := &types.ProcessID{
		Address: common.Address{2},
		Nonce:   2,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	// Create processes
	err = st.NewProcess(createTestProcess(process1))
	c.Assert(err, qt.IsNil)
	err = st.NewProcess(createTestProcess(process2))
	c.Assert(err, qt.IsNil)

	// Test 3: Push ballots to process1 and verify total pending increases
	ballot1 := &Ballot{
		ProcessID: process1.Marshal(),
		Address:   big.NewInt(1000),
		VoteID:    fmt.Appendf(nil, "vote1"),
	}
	ballot2 := &Ballot{
		ProcessID: process1.Marshal(),
		Address:   big.NewInt(1001),
		VoteID:    fmt.Appendf(nil, "vote2"),
	}

	err = st.PushPendingBallot(ballot1)
	c.Assert(err, qt.IsNil)
	total = st.TotalPendingBallots()
	c.Assert(total, qt.Equals, 1)

	err = st.PushPendingBallot(ballot2)
	c.Assert(err, qt.IsNil)
	total = st.TotalPendingBallots()
	c.Assert(total, qt.Equals, 2)

	// Test 4: Push ballots to process2
	ballot3 := &Ballot{
		ProcessID: process2.Marshal(),
		Address:   big.NewInt(1002),
		VoteID:    fmt.Appendf(nil, "vote3"),
	}

	err = st.PushPendingBallot(ballot3)
	c.Assert(err, qt.IsNil)
	total = st.TotalPendingBallots()
	c.Assert(total, qt.Equals, 3)

	// Test 5: Process a ballot and verify total pending decreases
	b1, key1, err := st.NextPendingBallot()
	c.Assert(err, qt.IsNil)

	verifiedBallot1 := &VerifiedBallot{
		ProcessID:   b1.ProcessID,
		VoteID:      b1.VoteID,
		VoterWeight: big.NewInt(1),
	}
	err = st.MarkBallotVerified(key1, verifiedBallot1)
	c.Assert(err, qt.IsNil)

	total = st.TotalPendingBallots()
	c.Assert(total, qt.Equals, 2)

	// Test 6: Process remaining ballots
	b2, key2, err := st.NextPendingBallot()
	c.Assert(err, qt.IsNil)
	verifiedBallot2 := &VerifiedBallot{
		ProcessID:   b2.ProcessID,
		VoteID:      b2.VoteID,
		VoterWeight: big.NewInt(1),
	}
	err = st.MarkBallotVerified(key2, verifiedBallot2)
	c.Assert(err, qt.IsNil)

	total = st.TotalPendingBallots()
	c.Assert(total, qt.Equals, 1)

	b3, key3, err := st.NextPendingBallot()
	c.Assert(err, qt.IsNil)
	verifiedBallot3 := &VerifiedBallot{
		ProcessID:   b3.ProcessID,
		VoteID:      b3.VoteID,
		VoterWeight: big.NewInt(1),
	}
	err = st.MarkBallotVerified(key3, verifiedBallot3)
	c.Assert(err, qt.IsNil)

	total = st.TotalPendingBallots()
	c.Assert(total, qt.Equals, 0)

	t.Logf("Successfully tested TotalPendingBallots functionality")
}

// TestTotalStatsConcurrency tests that total stats remain consistent under concurrent updates
func TestTotalStatsConcurrency(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create multiple processes
	numProcesses := 5
	processes := make([]*types.ProcessID, numProcesses)
	for i := range numProcesses {
		processes[i] = &types.ProcessID{
			Address: common.Address{byte(i + 1)},
			Nonce:   uint64(i + 1),
			Version: []byte{0x00, 0x00, 0x00, 0x01},
		}
		err = st.NewProcess(createTestProcess(processes[i]))
		c.Assert(err, qt.IsNil)
	}

	// Run concurrent updates
	numGoroutines := 10
	updatesPerGoroutine := 20
	wg := sync.WaitGroup{}

	for i := range numGoroutines {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			for j := 0; j < updatesPerGoroutine; j++ {
				// Pick a random process
				processIdx := (routineID + j) % numProcesses
				processID := processes[processIdx]

				// Update various stats
				updates := []ProcessStatsUpdate{
					{TypeStats: types.TypeStatsStateTransitions, Delta: 1},
					{TypeStats: types.TypeStatsVerifiedVotes, Delta: 2},
					{TypeStats: types.TypeStatsAggregatedVotes, Delta: 1},
				}

				// Lock before calling updateProcessStats (as it expects caller to hold the lock)
				st.globalLock.Lock()
				if err := st.updateProcessStats(processID.Marshal(), updates); err != nil {
					st.globalLock.Unlock()
					panic(err)
				}
				st.globalLock.Unlock()
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify final total stats
	totalStats, err := st.TotalStats()
	c.Assert(err, qt.IsNil)

	expectedStateTransitions := numGoroutines * updatesPerGoroutine * 1
	expectedVerifiedVotes := numGoroutines * updatesPerGoroutine * 2
	expectedAggregatedVotes := numGoroutines * updatesPerGoroutine * 1

	c.Assert(totalStats.StateTransitionCount, qt.Equals, expectedStateTransitions)
	c.Assert(totalStats.VerifiedVotesCount, qt.Equals, expectedVerifiedVotes)
	c.Assert(totalStats.AggregatedVotesCount, qt.Equals, expectedAggregatedVotes)

	t.Logf("Concurrent total stats update successful: stateTransitions=%d, verified=%d, aggregated=%d",
		totalStats.StateTransitionCount, totalStats.VerifiedVotesCount, totalStats.AggregatedVotesCount)
}

// TestTotalPendingBallotsIntegration tests that TotalPendingBallots integrates correctly
// with the existing ballot processing workflow
func TestTotalPendingBallotsIntegration(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create processes
	process1 := &types.ProcessID{
		Address: common.Address{1},
		Nonce:   1,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}
	process2 := &types.ProcessID{
		Address: common.Address{2},
		Nonce:   2,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	err = st.NewProcess(createTestProcess(process1))
	c.Assert(err, qt.IsNil)
	err = st.NewProcess(createTestProcess(process2))
	c.Assert(err, qt.IsNil)

	// Test the full workflow: push -> process -> aggregate
	// Step 1: Push multiple ballots
	numBallotsP1 := 5
	numBallotsP2 := 3

	for i := range numBallotsP1 {
		ballot := &Ballot{
			ProcessID: process1.Marshal(),
			Address:   big.NewInt(int64(i + 1000)),
			VoteID:    fmt.Appendf(nil, "vote%d", i),
		}
		err = st.PushPendingBallot(ballot)
		c.Assert(err, qt.IsNil)
	}

	for i := range numBallotsP2 {
		ballot := &Ballot{
			ProcessID: process2.Marshal(),
			Address:   big.NewInt(int64(i + 3000)),
			VoteID:    fmt.Appendf(nil, "vote%d", i+numBallotsP1),
		}
		err = st.PushPendingBallot(ballot)
		c.Assert(err, qt.IsNil)
	}

	// Verify initial total pending
	c.Assert(st.TotalPendingBallots(), qt.Equals, numBallotsP1+numBallotsP2)

	// Step 2: Process some ballots
	processedCount := 0
	for range 4 { // Process 4 ballots
		b, key, err := st.NextPendingBallot()
		c.Assert(err, qt.IsNil)

		verifiedBallot := &VerifiedBallot{
			ProcessID:   b.ProcessID,
			Address:     b.Address,
			VoteID:      b.VoteID,
			VoterWeight: big.NewInt(1),
		}
		err = st.MarkBallotVerified(key, verifiedBallot)
		c.Assert(err, qt.IsNil)
		processedCount++
	}

	// Verify total pending decreased
	c.Assert(st.TotalPendingBallots(), qt.Equals, numBallotsP1+numBallotsP2-processedCount)

	// Step 3: Get total stats
	totalStats, err := st.TotalStats()
	c.Assert(err, qt.IsNil)
	c.Assert(totalStats.VerifiedVotesCount, qt.Equals, processedCount)

	// Step 4: Aggregate verified ballots from process1
	// First check which process has verified ballots
	p1Count := st.CountVerifiedBallots(process1.Marshal())
	if p1Count > 0 {
		verifiedBallots, keys, err := st.PullVerifiedBallots(process1.Marshal(), p1Count)
		c.Assert(err, qt.IsNil)

		// Create aggregator batch
		aggBallots := make([]*AggregatorBallot, len(verifiedBallots))
		for i, vb := range verifiedBallots {
			aggBallots[i] = &AggregatorBallot{
				VoteID:  vb.VoteID,
				Address: vb.Address,
			}
		}

		batch := &AggregatorBallotBatch{
			ProcessID: process1.Marshal(),
			Ballots:   aggBallots,
		}

		err = st.PushAggregatorBatch(batch)
		c.Assert(err, qt.IsNil)

		err = st.MarkVerifiedBallotsDone(keys...)
		c.Assert(err, qt.IsNil)

		// Check total stats after aggregation
		totalStats, err = st.TotalStats()
		c.Assert(err, qt.IsNil)
		c.Assert(totalStats.AggregatedVotesCount, qt.Equals, len(verifiedBallots))
	}

	t.Logf("Integration test completed: totalPending=%d, totalVerified=%d, totalAggregated=%d",
		st.TotalPendingBallots(), totalStats.VerifiedVotesCount, totalStats.AggregatedVotesCount)
}
