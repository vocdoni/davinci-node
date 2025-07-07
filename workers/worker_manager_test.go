package workers

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
)

func TestNewWorkerManager(t *testing.T) {
	c := qt.New(t)

	rules := &WorkerBanRules{
		BanTimeout:          5 * time.Minute,
		FailuresToGetBanned: 5,
	}

	wm := NewWorkerManager(rules)

	c.Assert(wm, qt.IsNotNil)
	c.Assert(wm.rules, qt.Equals, rules)
	c.Assert(wm.innerCtx, qt.IsNil) // Should be nil until start() is called
	c.Assert(wm.cancelFunc, qt.IsNil)
}

func TestWorkerIsBanned(t *testing.T) {
	c := qt.New(t)

	rules := &WorkerBanRules{
		BanTimeout:          3 * time.Minute,
		FailuresToGetBanned: 3,
	}

	tests := []struct {
		name             string
		consecutiveFails int
		expected         bool
	}{
		{
			name:             "No failures",
			consecutiveFails: 0,
			expected:         false,
		},
		{
			name:             "Below threshold",
			consecutiveFails: 2,
			expected:         false,
		},
		{
			name:             "At threshold",
			consecutiveFails: 3,
			expected:         false,
		},
		{
			name:             "Above threshold",
			consecutiveFails: 4,
			expected:         true,
		},
		{
			name:             "Well above threshold",
			consecutiveFails: 10,
			expected:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			worker := &Worker{
				ID:               "test-worker",
				consecutiveFails: int64(tt.consecutiveFails),
			}

			result := worker.IsBanned(rules)
			c.Assert(result, qt.Equals, tt.expected)
		})
	}
}

func TestWorkerManagerAddWorker(t *testing.T) {
	c := qt.New(t)

	wm := NewWorkerManager(DefaultWorkerBanRules)

	// Test adding a new worker
	worker1 := wm.AddWorker("worker1")
	c.Assert(worker1, qt.IsNotNil)
	c.Assert(worker1.ID, qt.Equals, "worker1")
	c.Assert(worker1.SetConsecutiveFails(), qt.Equals, 0)
	c.Assert(worker1.GetBannedUntil().IsZero(), qt.IsTrue)

	// Test adding the same worker again (should return existing)
	worker1Again := wm.AddWorker("worker1")
	c.Assert(worker1Again, qt.Equals, worker1)

	// Test adding a different worker
	worker2 := wm.AddWorker("worker2")
	c.Assert(worker2, qt.IsNotNil)
	c.Assert(worker2.ID, qt.Equals, "worker2")
	c.Assert(worker2, qt.Not(qt.Equals), worker1)
}

func TestWorkerManagerGetWorker(t *testing.T) {
	c := qt.New(t)

	wm := NewWorkerManager(DefaultWorkerBanRules)

	// Test getting non-existent worker
	worker, exists := wm.GetWorker("nonexistent")
	c.Assert(worker, qt.IsNil)
	c.Assert(exists, qt.IsFalse)

	// Add a worker and test getting it
	addedWorker := wm.AddWorker("test-worker")
	retrievedWorker, exists := wm.GetWorker("test-worker")
	c.Assert(retrievedWorker, qt.IsNotNil)
	c.Assert(exists, qt.IsTrue)
	c.Assert(retrievedWorker, qt.Equals, addedWorker)
}

func TestWorkerManagerWorkerResult(t *testing.T) {
	c := qt.New(t)

	wm := NewWorkerManager(DefaultWorkerBanRules)

	// Test success result on new worker
	wm.WorkerResult("worker1", true)
	worker, exists := wm.GetWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker.SetConsecutiveFails(), qt.Equals, 0)

	// Test failure result
	wm.WorkerResult("worker1", false)
	worker, exists = wm.GetWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker.SetConsecutiveFails(), qt.Equals, 1)

	// Test multiple failures
	wm.WorkerResult("worker1", false)
	wm.WorkerResult("worker1", false)
	worker, exists = wm.GetWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker.SetConsecutiveFails(), qt.Equals, 3)

	// Test success resets failures
	wm.WorkerResult("worker1", true)
	worker, exists = wm.GetWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker.SetConsecutiveFails(), qt.Equals, 0)
}

func TestWorkerManagerBannedWorkers(t *testing.T) {
	c := qt.New(t)

	rules := &WorkerBanRules{
		BanTimeout:          3 * time.Minute,
		FailuresToGetBanned: 2,
	}
	wm := NewWorkerManager(rules)

	// Initially no banned workers
	banned := wm.BannedWorkers()
	c.Assert(len(banned), qt.Equals, 0)

	// Add workers with different failure counts
	wm.AddWorker("worker1")
	wm.WorkerResult("worker1", false) // 1 failure
	wm.WorkerResult("worker1", false) // 2 failures

	wm.AddWorker("worker2")
	wm.WorkerResult("worker2", false) // 1 failure
	wm.WorkerResult("worker2", false) // 2 failures
	wm.WorkerResult("worker2", false) // 3 failures - should be banned

	wm.AddWorker("worker3")
	wm.WorkerResult("worker3", true) // success

	banned = wm.BannedWorkers()
	c.Assert(len(banned), qt.Equals, 1)
	c.Assert(banned[0].ID, qt.Equals, "worker2")
}

func TestWorkerManagerResetWorker(t *testing.T) {
	c := qt.New(t)

	wm := NewWorkerManager(DefaultWorkerBanRules)

	// Add worker with failures
	wm.AddWorker("worker1")
	wm.WorkerResult("worker1", false)
	wm.WorkerResult("worker1", false)

	worker, _ := wm.GetWorker("worker1")
	c.Assert(worker.SetConsecutiveFails(), qt.Equals, 2)

	// Reset worker
	wm.ResetWorker("worker1")

	worker, exists := wm.GetWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker.SetConsecutiveFails(), qt.Equals, 0)
	c.Assert(worker.GetBannedUntil().IsZero(), qt.IsTrue)

	// Test resetting non-existent worker (should not panic)
	wm.ResetWorker("nonexistent")
}

func TestWorkerManagerSetBanDuration(t *testing.T) {
	c := qt.New(t)

	rules := &WorkerBanRules{
		BanTimeout:          5 * time.Minute,
		FailuresToGetBanned: 3,
	}
	wm := NewWorkerManager(rules)

	// Add worker
	wm.AddWorker("worker1")

	// Set ban duration
	beforeBan := time.Now()
	wm.SetBanDuration("worker1")
	afterBan := time.Now()

	worker, exists := wm.GetWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	bannedUntil := worker.GetBannedUntil()
	c.Assert(bannedUntil.After(beforeBan.Add(4*time.Minute)), qt.IsTrue)
	c.Assert(bannedUntil.Before(afterBan.Add(6*time.Minute)), qt.IsTrue)

	// Test setting ban on non-existent worker (should not panic)
	wm.SetBanDuration("nonexistent")
}

func TestWorkerManagerStartStop(t *testing.T) {
	c := qt.New(t)

	wm := NewWorkerManager(DefaultWorkerBanRules)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test start
	wm.Start(ctx)
	c.Assert(wm.innerCtx, qt.IsNotNil)
	c.Assert(wm.cancelFunc, qt.IsNotNil)

	// Add a worker to verify it exists
	wm.AddWorker("test-worker")
	worker, exists := wm.GetWorker("test-worker")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker, qt.IsNotNil)

	// Test stop
	wm.Stop()

	// Verify workers are cleared
	worker, exists = wm.GetWorker("test-worker")
	c.Assert(exists, qt.IsFalse)
	c.Assert(worker, qt.IsNil)
}

func TestWorkerManagerBanUnbanCycle(t *testing.T) {
	c := qt.New(t)

	// Use short timeout for testing
	rules := &WorkerBanRules{
		BanTimeout:          100 * time.Millisecond,
		FailuresToGetBanned: 2,
	}
	wm := NewWorkerManager(rules)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wm.Start(ctx)
	defer wm.Stop()

	// Add worker and make it fail enough to be banned
	wm.AddWorker("worker1")
	wm.WorkerResult("worker1", false) // 1 failure
	wm.WorkerResult("worker1", false) // 2 failures
	wm.WorkerResult("worker1", false) // 3 failures - should be banned

	// Verify worker is banned
	banned := wm.BannedWorkers()
	c.Assert(len(banned), qt.Equals, 1)

	// Manually trigger ban duration setting (since ticker runs every 10 seconds)
	wm.SetBanDuration("worker1")

	worker, exists := wm.GetWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker.GetBannedUntil().IsZero(), qt.IsFalse)

	// Wait for ban to expire
	time.Sleep(150 * time.Millisecond)

	// Manually trigger worker reset (since ticker runs every 10 seconds)
	wm.ResetWorker("worker1")

	worker, exists = wm.GetWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker.SetConsecutiveFails(), qt.Equals, 0)
	c.Assert(worker.GetBannedUntil().IsZero(), qt.IsTrue)
}

func TestWorkerManagerRealStartMethodWithConfigurableTicker(t *testing.T) {
	c := qt.New(t)

	t.Run("Real start method with fast ticker - complete ban/unban cycle", func(t *testing.T) {
		// Test the actual start() method with configurable fast ticker
		rules := &WorkerBanRules{
			BanTimeout:          200 * time.Millisecond, // Short timeout for testing
			FailuresToGetBanned: 1,
		}
		// Use REAL start method with fast ticker interval
		wm := NewWorkerManager(rules, 100*time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Use the REAL start method - no modifications, just fast ticker
		wm.Start(ctx)
		defer wm.Stop()

		// Set up workers in banned state
		wm.AddWorker("worker1")
		wm.AddWorker("worker2")
		wm.WorkerResult("worker1", false) // 1 failure
		wm.WorkerResult("worker1", false) // 2 failures - should be banned
		wm.WorkerResult("worker2", false) // 1 failure
		wm.WorkerResult("worker2", false) // 2 failures - should be banned

		// Verify workers are banned but no ban duration set yet
		banned := wm.BannedWorkers()
		c.Assert(len(banned), qt.Equals, 2)

		worker1, exists1 := wm.GetWorker("worker1")
		worker2, exists2 := wm.GetWorker("worker2")
		c.Assert(exists1, qt.IsTrue)
		c.Assert(exists2, qt.IsTrue)
		c.Assert(worker1.GetBannedUntil().IsZero(), qt.IsTrue) // No ban duration set yet
		c.Assert(worker2.GetBannedUntil().IsZero(), qt.IsTrue) // No ban duration set yet

		// Wait for the REAL ticker to fire (100ms + buffer)
		time.Sleep(150 * time.Millisecond)

		// Verify ban durations were set by the real background logic
		worker1, _ = wm.GetWorker("worker1")
		worker2, _ = wm.GetWorker("worker2")
		c.Assert(worker1.GetBannedUntil().IsZero(), qt.IsFalse, qt.Commentf("Ban duration should be set after ticker"))
		c.Assert(worker2.GetBannedUntil().IsZero(), qt.IsFalse, qt.Commentf("Ban duration should be set after ticker"))

		// Wait for bans to expire (200ms) + next ticker (100ms)
		time.Sleep(350 * time.Millisecond)

		// Verify workers were reset by the real background logic
		worker1, exists1 = wm.GetWorker("worker1")
		worker2, exists2 = wm.GetWorker("worker2")
		c.Assert(exists1, qt.IsTrue)
		c.Assert(exists2, qt.IsTrue)
		c.Assert(worker1.SetConsecutiveFails(), qt.Equals, 0, qt.Commentf("Worker should be reset after ban expiry"))
		c.Assert(worker2.SetConsecutiveFails(), qt.Equals, 0, qt.Commentf("Worker should be reset after ban expiry"))
		c.Assert(worker1.GetBannedUntil().IsZero(), qt.IsTrue, qt.Commentf("Ban should be cleared after reset"))
		c.Assert(worker2.GetBannedUntil().IsZero(), qt.IsTrue, qt.Commentf("Ban should be cleared after reset"))
	})

	t.Run("Real start method - context cancellation behavior", func(t *testing.T) {
		// Test real start method context cancellation
		rules := &WorkerBanRules{
			BanTimeout:          1 * time.Second,
			FailuresToGetBanned: 1,
		}
		wm := NewWorkerManager(rules, 50*time.Millisecond) // Fast ticker

		ctx, cancel := context.WithCancel(context.Background())

		// Use the REAL start method
		wm.Start(ctx)

		// Verify initialization
		c.Assert(wm.innerCtx, qt.IsNotNil)
		c.Assert(wm.cancelFunc, qt.IsNotNil)

		// Add workers
		wm.AddWorker("worker1")
		wm.AddWorker("worker2")

		// Verify workers exist
		_, exists1 := wm.GetWorker("worker1")
		_, exists2 := wm.GetWorker("worker2")
		c.Assert(exists1, qt.IsTrue)
		c.Assert(exists2, qt.IsTrue)

		// Cancel context - this should trigger the real ctx.Done() case
		cancel()

		// Give time for the real goroutine to process cancellation
		time.Sleep(100 * time.Millisecond)

		// Verify the real stop() was called and workers cleared
		_, exists1 = wm.GetWorker("worker1")
		_, exists2 = wm.GetWorker("worker2")
		c.Assert(exists1, qt.IsFalse)
		c.Assert(exists2, qt.IsFalse)
	})

	t.Run("Real start method - ticker interval verification", func(t *testing.T) {
		// Test that different ticker intervals work correctly
		rules := &WorkerBanRules{
			BanTimeout:          100 * time.Millisecond,
			FailuresToGetBanned: 1,
		}

		// Test with custom interval
		wm := NewWorkerManager(rules, 50*time.Millisecond)
		c.Assert(wm.tickerInterval, qt.Equals, 50*time.Millisecond)

		// Test with default interval
		wmDefault := NewWorkerManager(rules)
		c.Assert(wmDefault.tickerInterval, qt.Equals, 10*time.Second)

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		// Start the fast ticker version
		wm.Start(ctx)
		defer wm.Stop()

		// Add banned worker
		wm.AddWorker("worker1")
		wm.WorkerResult("worker1", false)
		wm.WorkerResult("worker1", false) // Should be banned

		// Verify banned
		banned := wm.BannedWorkers()
		c.Assert(len(banned), qt.Equals, 1)

		// Wait for ticker to process (should happen within 50ms + buffer)
		time.Sleep(100 * time.Millisecond)

		// Verify ban duration was set by real ticker
		worker, exists := wm.GetWorker("worker1")
		c.Assert(exists, qt.IsTrue)
		c.Assert(worker.GetBannedUntil().IsZero(), qt.IsFalse)
	})

	t.Run("Real start method - no banned workers scenario", func(t *testing.T) {
		// Test ticker behavior when no workers are banned
		rules := &WorkerBanRules{
			BanTimeout:          200 * time.Millisecond,
			FailuresToGetBanned: 5, // High threshold
		}
		wm := NewWorkerManager(rules, 50*time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()

		wm.Start(ctx)
		defer wm.Stop()

		// Add workers with some failures but not enough to ban
		wm.AddWorker("worker1")
		wm.AddWorker("worker2")
		wm.WorkerResult("worker1", false) // 1 failure
		wm.WorkerResult("worker2", false) // 1 failure

		// Wait for multiple ticker cycles
		time.Sleep(200 * time.Millisecond)

		// Verify workers are still active and not banned
		worker1, exists1 := wm.GetWorker("worker1")
		worker2, exists2 := wm.GetWorker("worker2")
		c.Assert(exists1, qt.IsTrue)
		c.Assert(exists2, qt.IsTrue)
		c.Assert(worker1.SetConsecutiveFails(), qt.Equals, 1)
		c.Assert(worker2.SetConsecutiveFails(), qt.Equals, 1)
		c.Assert(worker1.GetBannedUntil().IsZero(), qt.IsTrue)
		c.Assert(worker2.GetBannedUntil().IsZero(), qt.IsTrue)

		// Verify no workers are banned
		banned := wm.BannedWorkers()
		c.Assert(len(banned), qt.Equals, 0)
	})
}

func TestWorkerManagerConcurrency(t *testing.T) {
	c := qt.New(t)

	wm := NewWorkerManager(DefaultWorkerBanRules)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wm.Start(ctx)
	defer wm.Stop()

	const numWorkers = 100
	const numOperations = 10

	var wg sync.WaitGroup

	// Concurrent worker additions
	for i := range numWorkers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			workerID := fmt.Sprintf("worker-%d", id)
			wm.AddWorker(workerID)

			// Perform some operations on the worker
			for j := range numOperations {
				wm.WorkerResult(workerID, j%2 == 0) // Alternate success/failure
			}
		}(i)
	}

	// Concurrent banned workers checks
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wm.BannedWorkers()
		}()
	}

	wg.Wait()

	// Verify all workers were added
	for i := range numWorkers {
		workerID := fmt.Sprintf("worker-%d", i)
		worker, exists := wm.GetWorker(workerID)
		c.Assert(exists, qt.IsTrue)
		c.Assert(worker.ID, qt.Equals, workerID)
	}
}

func TestWorkerManagerContextCancellation(t *testing.T) {
	c := qt.New(t)

	wm := NewWorkerManager(DefaultWorkerBanRules)
	ctx, cancel := context.WithCancel(context.Background())

	wm.Start(ctx)
	c.Assert(wm.innerCtx, qt.IsNotNil)

	// Add a worker
	wm.AddWorker("test-worker")
	_, exists := wm.GetWorker("test-worker")
	c.Assert(exists, qt.IsTrue)

	// Cancel context
	cancel()

	// Give some time for the goroutine to process the cancellation
	time.Sleep(50 * time.Millisecond)

	// Worker should be cleared because context cancellation calls stop() which clears workers
	_, exists = wm.GetWorker("test-worker")
	c.Assert(exists, qt.IsFalse)
}

func TestWorkerManagerEdgeCases(t *testing.T) {
	c := qt.New(t)

	t.Run("Zero max consecutive fails", func(t *testing.T) {
		rules := &WorkerBanRules{
			BanTimeout:          1 * time.Minute,
			FailuresToGetBanned: 0,
		}
		wm := NewWorkerManager(rules)

		wm.AddWorker("worker1")
		wm.WorkerResult("worker1", false) // 1 failure

		worker, _ := wm.GetWorker("worker1")
		c.Assert(worker.IsBanned(rules), qt.IsTrue) // Should be banned immediately
	})

	t.Run("Negative max consecutive fails", func(t *testing.T) {
		rules := &WorkerBanRules{
			BanTimeout:          1 * time.Minute,
			FailuresToGetBanned: -1,
		}
		wm := NewWorkerManager(rules)

		wm.AddWorker("worker1")
		worker, _ := wm.GetWorker("worker1")
		c.Assert(worker.IsBanned(rules), qt.IsTrue) // Should be banned immediately
	})

	t.Run("Empty worker ID", func(t *testing.T) {
		wm := NewWorkerManager(DefaultWorkerBanRules)

		worker := wm.AddWorker("")
		c.Assert(worker.ID, qt.Equals, "")

		retrievedWorker, exists := wm.GetWorker("")
		c.Assert(exists, qt.IsTrue)
		c.Assert(retrievedWorker, qt.Equals, worker)
	})
}
