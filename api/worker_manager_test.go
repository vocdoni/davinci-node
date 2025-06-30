package api

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

	rules := banRules{
		timeout:             5 * time.Minute,
		maxConsecutiveFails: 5,
	}

	wm := newWorkerManager(rules)

	c.Assert(wm, qt.IsNotNil)
	c.Assert(wm.rules, qt.Equals, rules)
	c.Assert(wm.innerCtx, qt.IsNil) // Should be nil until start() is called
	c.Assert(wm.cancelFunc, qt.IsNil)
}

func TestWorkerIsBanned(t *testing.T) {
	c := qt.New(t)

	rules := banRules{
		timeout:             3 * time.Minute,
		maxConsecutiveFails: 3,
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
			worker := &worker{
				ID:               "test-worker",
				consecutiveFails: int64(tt.consecutiveFails),
			}

			result := worker.isBanned(rules)
			c.Assert(result, qt.Equals, tt.expected)
		})
	}
}

func TestWorkerManagerAddWorker(t *testing.T) {
	c := qt.New(t)

	wm := newWorkerManager(defaultBanRules)

	// Test adding a new worker
	worker1 := wm.addWorker("worker1")
	c.Assert(worker1, qt.IsNotNil)
	c.Assert(worker1.ID, qt.Equals, "worker1")
	c.Assert(worker1.getConsecutiveFails(), qt.Equals, 0)
	c.Assert(worker1.getBannedUntil().IsZero(), qt.IsTrue)

	// Test adding the same worker again (should return existing)
	worker1Again := wm.addWorker("worker1")
	c.Assert(worker1Again, qt.Equals, worker1)

	// Test adding a different worker
	worker2 := wm.addWorker("worker2")
	c.Assert(worker2, qt.IsNotNil)
	c.Assert(worker2.ID, qt.Equals, "worker2")
	c.Assert(worker2, qt.Not(qt.Equals), worker1)
}

func TestWorkerManagerGetWorker(t *testing.T) {
	c := qt.New(t)

	wm := newWorkerManager(defaultBanRules)

	// Test getting non-existent worker
	worker, exists := wm.getWorker("nonexistent")
	c.Assert(worker, qt.IsNil)
	c.Assert(exists, qt.IsFalse)

	// Add a worker and test getting it
	addedWorker := wm.addWorker("test-worker")
	retrievedWorker, exists := wm.getWorker("test-worker")
	c.Assert(retrievedWorker, qt.IsNotNil)
	c.Assert(exists, qt.IsTrue)
	c.Assert(retrievedWorker, qt.Equals, addedWorker)
}

func TestWorkerManagerWorkerResult(t *testing.T) {
	c := qt.New(t)

	wm := newWorkerManager(defaultBanRules)

	// Test success result on new worker
	wm.workerResult("worker1", true)
	worker, exists := wm.getWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker.getConsecutiveFails(), qt.Equals, 0)

	// Test failure result
	wm.workerResult("worker1", false)
	worker, exists = wm.getWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker.getConsecutiveFails(), qt.Equals, 1)

	// Test multiple failures
	wm.workerResult("worker1", false)
	wm.workerResult("worker1", false)
	worker, exists = wm.getWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker.getConsecutiveFails(), qt.Equals, 3)

	// Test success resets failures
	wm.workerResult("worker1", true)
	worker, exists = wm.getWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker.getConsecutiveFails(), qt.Equals, 0)
}

func TestWorkerManagerBannedWorkers(t *testing.T) {
	c := qt.New(t)

	rules := banRules{
		timeout:             3 * time.Minute,
		maxConsecutiveFails: 2,
	}
	wm := newWorkerManager(rules)

	// Initially no banned workers
	banned := wm.bannedWorkers()
	c.Assert(len(banned), qt.Equals, 0)

	// Add workers with different failure counts
	wm.addWorker("worker1")
	wm.workerResult("worker1", false) // 1 failure
	wm.workerResult("worker1", false) // 2 failures

	wm.addWorker("worker2")
	wm.workerResult("worker2", false) // 1 failure
	wm.workerResult("worker2", false) // 2 failures
	wm.workerResult("worker2", false) // 3 failures - should be banned

	wm.addWorker("worker3")
	wm.workerResult("worker3", true) // success

	banned = wm.bannedWorkers()
	c.Assert(len(banned), qt.Equals, 1)
	c.Assert(banned[0].ID, qt.Equals, "worker2")
}

func TestWorkerManagerResetWorker(t *testing.T) {
	c := qt.New(t)

	wm := newWorkerManager(defaultBanRules)

	// Add worker with failures
	wm.addWorker("worker1")
	wm.workerResult("worker1", false)
	wm.workerResult("worker1", false)

	worker, _ := wm.getWorker("worker1")
	c.Assert(worker.getConsecutiveFails(), qt.Equals, 2)

	// Reset worker
	wm.resetWorker("worker1")

	worker, exists := wm.getWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker.getConsecutiveFails(), qt.Equals, 0)
	c.Assert(worker.getBannedUntil().IsZero(), qt.IsTrue)

	// Test resetting non-existent worker (should not panic)
	wm.resetWorker("nonexistent")
}

func TestWorkerManagerSetBanDuration(t *testing.T) {
	c := qt.New(t)

	rules := banRules{
		timeout:             5 * time.Minute,
		maxConsecutiveFails: 3,
	}
	wm := newWorkerManager(rules)

	// Add worker
	wm.addWorker("worker1")

	// Set ban duration
	beforeBan := time.Now()
	wm.setBanDuration("worker1")
	afterBan := time.Now()

	worker, exists := wm.getWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	bannedUntil := worker.getBannedUntil()
	c.Assert(bannedUntil.After(beforeBan.Add(4*time.Minute)), qt.IsTrue)
	c.Assert(bannedUntil.Before(afterBan.Add(6*time.Minute)), qt.IsTrue)

	// Test setting ban on non-existent worker (should not panic)
	wm.setBanDuration("nonexistent")
}

func TestWorkerManagerStartStop(t *testing.T) {
	c := qt.New(t)

	wm := newWorkerManager(defaultBanRules)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test start
	wm.start(ctx)
	c.Assert(wm.innerCtx, qt.IsNotNil)
	c.Assert(wm.cancelFunc, qt.IsNotNil)

	// Add a worker to verify it exists
	wm.addWorker("test-worker")
	worker, exists := wm.getWorker("test-worker")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker, qt.IsNotNil)

	// Test stop
	wm.stop()

	// Verify workers are cleared
	worker, exists = wm.getWorker("test-worker")
	c.Assert(exists, qt.IsFalse)
	c.Assert(worker, qt.IsNil)
}

func TestWorkerManagerBanUnbanCycle(t *testing.T) {
	c := qt.New(t)

	// Use short timeout for testing
	rules := banRules{
		timeout:             100 * time.Millisecond,
		maxConsecutiveFails: 2,
	}
	wm := newWorkerManager(rules)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wm.start(ctx)
	defer wm.stop()

	// Add worker and make it fail enough to be banned
	wm.addWorker("worker1")
	wm.workerResult("worker1", false) // 1 failure
	wm.workerResult("worker1", false) // 2 failures
	wm.workerResult("worker1", false) // 3 failures - should be banned

	// Verify worker is banned
	banned := wm.bannedWorkers()
	c.Assert(len(banned), qt.Equals, 1)

	// Manually trigger ban duration setting (since ticker runs every 10 seconds)
	wm.setBanDuration("worker1")

	worker, exists := wm.getWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker.getBannedUntil().IsZero(), qt.IsFalse)

	// Wait for ban to expire
	time.Sleep(150 * time.Millisecond)

	// Manually trigger worker reset (since ticker runs every 10 seconds)
	wm.resetWorker("worker1")

	worker, exists = wm.getWorker("worker1")
	c.Assert(exists, qt.IsTrue)
	c.Assert(worker.getConsecutiveFails(), qt.Equals, 0)
	c.Assert(worker.getBannedUntil().IsZero(), qt.IsTrue)
}

func TestWorkerManagerRealStartMethodWithConfigurableTicker(t *testing.T) {
	c := qt.New(t)

	t.Run("Real start method with fast ticker - complete ban/unban cycle", func(t *testing.T) {
		// Test the actual start() method with configurable fast ticker
		rules := banRules{
			timeout:             200 * time.Millisecond, // Short timeout for testing
			maxConsecutiveFails: 1,
		}
		// Use REAL start method with fast ticker interval
		wm := newWorkerManager(rules, 100*time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Use the REAL start method - no modifications, just fast ticker
		wm.start(ctx)
		defer wm.stop()

		// Set up workers in banned state
		wm.addWorker("worker1")
		wm.addWorker("worker2")
		wm.workerResult("worker1", false) // 1 failure
		wm.workerResult("worker1", false) // 2 failures - should be banned
		wm.workerResult("worker2", false) // 1 failure
		wm.workerResult("worker2", false) // 2 failures - should be banned

		// Verify workers are banned but no ban duration set yet
		banned := wm.bannedWorkers()
		c.Assert(len(banned), qt.Equals, 2)
		
		worker1, exists1 := wm.getWorker("worker1")
		worker2, exists2 := wm.getWorker("worker2")
		c.Assert(exists1, qt.IsTrue)
		c.Assert(exists2, qt.IsTrue)
		c.Assert(worker1.getBannedUntil().IsZero(), qt.IsTrue) // No ban duration set yet
		c.Assert(worker2.getBannedUntil().IsZero(), qt.IsTrue) // No ban duration set yet

		// Wait for the REAL ticker to fire (100ms + buffer)
		time.Sleep(150 * time.Millisecond)

		// Verify ban durations were set by the real background logic
		worker1, _ = wm.getWorker("worker1")
		worker2, _ = wm.getWorker("worker2")
		c.Assert(worker1.getBannedUntil().IsZero(), qt.IsFalse, qt.Commentf("Ban duration should be set after ticker"))
		c.Assert(worker2.getBannedUntil().IsZero(), qt.IsFalse, qt.Commentf("Ban duration should be set after ticker"))

		// Wait for bans to expire (200ms) + next ticker (100ms)
		time.Sleep(350 * time.Millisecond)

		// Verify workers were reset by the real background logic
		worker1, exists1 = wm.getWorker("worker1")
		worker2, exists2 = wm.getWorker("worker2")
		c.Assert(exists1, qt.IsTrue)
		c.Assert(exists2, qt.IsTrue)
		c.Assert(worker1.getConsecutiveFails(), qt.Equals, 0, qt.Commentf("Worker should be reset after ban expiry"))
		c.Assert(worker2.getConsecutiveFails(), qt.Equals, 0, qt.Commentf("Worker should be reset after ban expiry"))
		c.Assert(worker1.getBannedUntil().IsZero(), qt.IsTrue, qt.Commentf("Ban should be cleared after reset"))
		c.Assert(worker2.getBannedUntil().IsZero(), qt.IsTrue, qt.Commentf("Ban should be cleared after reset"))
	})

	t.Run("Real start method - context cancellation behavior", func(t *testing.T) {
		// Test real start method context cancellation
		rules := banRules{
			timeout:             1 * time.Second,
			maxConsecutiveFails: 1,
		}
		wm := newWorkerManager(rules, 50*time.Millisecond) // Fast ticker

		ctx, cancel := context.WithCancel(context.Background())

		// Use the REAL start method
		wm.start(ctx)

		// Verify initialization
		c.Assert(wm.innerCtx, qt.IsNotNil)
		c.Assert(wm.cancelFunc, qt.IsNotNil)

		// Add workers
		wm.addWorker("worker1")
		wm.addWorker("worker2")
		
		// Verify workers exist
		_, exists1 := wm.getWorker("worker1")
		_, exists2 := wm.getWorker("worker2")
		c.Assert(exists1, qt.IsTrue)
		c.Assert(exists2, qt.IsTrue)

		// Cancel context - this should trigger the real ctx.Done() case
		cancel()

		// Give time for the real goroutine to process cancellation
		time.Sleep(100 * time.Millisecond)

		// Verify the real stop() was called and workers cleared
		_, exists1 = wm.getWorker("worker1")
		_, exists2 = wm.getWorker("worker2")
		c.Assert(exists1, qt.IsFalse)
		c.Assert(exists2, qt.IsFalse)
	})

	t.Run("Real start method - ticker interval verification", func(t *testing.T) {
		// Test that different ticker intervals work correctly
		rules := banRules{
			timeout:             100 * time.Millisecond,
			maxConsecutiveFails: 1,
		}

		// Test with custom interval
		wm := newWorkerManager(rules, 50*time.Millisecond)
		c.Assert(wm.tickerInterval, qt.Equals, 50*time.Millisecond)

		// Test with default interval
		wmDefault := newWorkerManager(rules)
		c.Assert(wmDefault.tickerInterval, qt.Equals, 10*time.Second)

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		// Start the fast ticker version
		wm.start(ctx)
		defer wm.stop()

		// Add banned worker
		wm.addWorker("worker1")
		wm.workerResult("worker1", false)
		wm.workerResult("worker1", false) // Should be banned

		// Verify banned
		banned := wm.bannedWorkers()
		c.Assert(len(banned), qt.Equals, 1)

		// Wait for ticker to process (should happen within 50ms + buffer)
		time.Sleep(100 * time.Millisecond)

		// Verify ban duration was set by real ticker
		worker, exists := wm.getWorker("worker1")
		c.Assert(exists, qt.IsTrue)
		c.Assert(worker.getBannedUntil().IsZero(), qt.IsFalse)
	})

	t.Run("Real start method - no banned workers scenario", func(t *testing.T) {
		// Test ticker behavior when no workers are banned
		rules := banRules{
			timeout:             200 * time.Millisecond,
			maxConsecutiveFails: 5, // High threshold
		}
		wm := newWorkerManager(rules, 50*time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()

		wm.start(ctx)
		defer wm.stop()

		// Add workers with some failures but not enough to ban
		wm.addWorker("worker1")
		wm.addWorker("worker2")
		wm.workerResult("worker1", false) // 1 failure
		wm.workerResult("worker2", false) // 1 failure

		// Wait for multiple ticker cycles
		time.Sleep(200 * time.Millisecond)

		// Verify workers are still active and not banned
		worker1, exists1 := wm.getWorker("worker1")
		worker2, exists2 := wm.getWorker("worker2")
		c.Assert(exists1, qt.IsTrue)
		c.Assert(exists2, qt.IsTrue)
		c.Assert(worker1.getConsecutiveFails(), qt.Equals, 1)
		c.Assert(worker2.getConsecutiveFails(), qt.Equals, 1)
		c.Assert(worker1.getBannedUntil().IsZero(), qt.IsTrue)
		c.Assert(worker2.getBannedUntil().IsZero(), qt.IsTrue)

		// Verify no workers are banned
		banned := wm.bannedWorkers()
		c.Assert(len(banned), qt.Equals, 0)
	})
}

func TestWorkerManagerConcurrency(t *testing.T) {
	c := qt.New(t)

	wm := newWorkerManager(defaultBanRules)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wm.start(ctx)
	defer wm.stop()

	const numWorkers = 100
	const numOperations = 10

	var wg sync.WaitGroup

	// Concurrent worker additions
	for i := range numWorkers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			workerID := fmt.Sprintf("worker-%d", id)
			wm.addWorker(workerID)

			// Perform some operations on the worker
			for j := 0; j < numOperations; j++ {
				wm.workerResult(workerID, j%2 == 0) // Alternate success/failure
			}
		}(i)
	}

	// Concurrent banned workers checks
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wm.bannedWorkers()
		}()
	}

	wg.Wait()

	// Verify all workers were added
	for i := range numWorkers {
		workerID := fmt.Sprintf("worker-%d", i)
		worker, exists := wm.getWorker(workerID)
		c.Assert(exists, qt.IsTrue)
		c.Assert(worker.ID, qt.Equals, workerID)
	}
}

func TestWorkerManagerContextCancellation(t *testing.T) {
	c := qt.New(t)

	wm := newWorkerManager(defaultBanRules)
	ctx, cancel := context.WithCancel(context.Background())

	wm.start(ctx)
	c.Assert(wm.innerCtx, qt.IsNotNil)

	// Add a worker
	wm.addWorker("test-worker")
	_, exists := wm.getWorker("test-worker")
	c.Assert(exists, qt.IsTrue)

	// Cancel context
	cancel()

	// Give some time for the goroutine to process the cancellation
	time.Sleep(50 * time.Millisecond)

	// Worker should be cleared because context cancellation calls stop() which clears workers
	_, exists = wm.getWorker("test-worker")
	c.Assert(exists, qt.IsFalse)
}

func TestWorkerManagerEdgeCases(t *testing.T) {
	c := qt.New(t)

	t.Run("Zero max consecutive fails", func(t *testing.T) {
		rules := banRules{
			timeout:             1 * time.Minute,
			maxConsecutiveFails: 0,
		}
		wm := newWorkerManager(rules)

		wm.addWorker("worker1")
		wm.workerResult("worker1", false) // 1 failure

		worker, _ := wm.getWorker("worker1")
		c.Assert(worker.isBanned(rules), qt.IsTrue) // Should be banned immediately
	})

	t.Run("Negative max consecutive fails", func(t *testing.T) {
		rules := banRules{
			timeout:             1 * time.Minute,
			maxConsecutiveFails: -1,
		}
		wm := newWorkerManager(rules)

		wm.addWorker("worker1")
		worker, _ := wm.getWorker("worker1")
		c.Assert(worker.isBanned(rules), qt.IsTrue) // Should be banned immediately
	})

	t.Run("Empty worker ID", func(t *testing.T) {
		wm := newWorkerManager(defaultBanRules)

		worker := wm.addWorker("")
		c.Assert(worker.ID, qt.Equals, "")

		retrievedWorker, exists := wm.getWorker("")
		c.Assert(exists, qt.IsTrue)
		c.Assert(retrievedWorker, qt.Equals, worker)
	})
}
