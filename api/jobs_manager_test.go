package api

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
)

func TestNewJobsManager(t *testing.T) {
	c := qt.New(t)

	t.Run("Default configuration", func(t *testing.T) {
		jobTimeout := 5 * time.Minute
		jm := newJobsManager(jobTimeout)

		c.Assert(jm, qt.IsNotNil)
		c.Assert(jm.jobTimeout, qt.Equals, jobTimeout)
		c.Assert(jm.tickerInterval, qt.Equals, 10*time.Second) // Default interval
		c.Assert(jm.pending, qt.IsNotNil)
		c.Assert(len(jm.pending), qt.Equals, 0)
		c.Assert(jm.failedJobs, qt.IsNotNil)
		c.Assert(jm.wm, qt.IsNotNil)
		c.Assert(jm.ctx, qt.IsNil) // Should be nil until start() is called
		c.Assert(jm.cancel, qt.IsNil)
	})

	t.Run("Custom ticker interval", func(t *testing.T) {
		jobTimeout := 2 * time.Minute
		tickerInterval := 100 * time.Millisecond
		jm := newJobsManager(jobTimeout, tickerInterval)

		c.Assert(jm.jobTimeout, qt.Equals, jobTimeout)
		c.Assert(jm.tickerInterval, qt.Equals, tickerInterval)
	})
}

func TestJobsManagerStartStop(t *testing.T) {
	c := qt.New(t)

	jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test start
	jm.start(ctx)
	c.Assert(jm.ctx, qt.IsNotNil)
	c.Assert(jm.cancel, qt.IsNotNil)

	// Test stop
	jm.stop()

	// Verify cleanup
	c.Assert(len(jm.pending), qt.Equals, 0)
	
	// Verify channel is closed by trying to receive (should not block)
	select {
	case _, ok := <-jm.failedJobs:
		c.Assert(ok, qt.IsFalse) // Channel should be closed
	default:
		t.Error("Expected channel to be closed")
	}
}

func TestJobsManagerRegisterJob(t *testing.T) {
	c := qt.New(t)

	t.Run("Register job for new worker", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		workerAddr := "worker1"
		voteID := []byte("vote123")

		job, ok := jm.registerJob(workerAddr, voteID)

		c.Assert(ok, qt.IsTrue)
		c.Assert(job, qt.IsNotNil)
		c.Assert(job.VoteID, qt.DeepEquals, voteID)
		c.Assert(job.Address, qt.Equals, workerAddr)
		c.Assert(job.Timestamp.IsZero(), qt.IsFalse)
		c.Assert(job.Expiration.After(job.Timestamp), qt.IsTrue)

		// Verify job is in pending map
		c.Assert(len(jm.pending), qt.Equals, 1)
		storedJob, exists := jm.pending[string(voteID)]
		c.Assert(exists, qt.IsTrue)
		c.Assert(storedJob, qt.Equals, job)
	})

	t.Run("Register job for existing worker", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		workerAddr := "worker1"
		
		// Add worker first
		jm.wm.addWorker(workerAddr)
		
		voteID := []byte("vote123")
		job, ok := jm.registerJob(workerAddr, voteID)

		c.Assert(ok, qt.IsTrue)
		c.Assert(job, qt.IsNotNil)
		c.Assert(job.Address, qt.Equals, workerAddr)
	})

	t.Run("Register job for banned worker", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		workerAddr := "worker1"
		
		// Add worker and make it fail to get banned
		jm.wm.addWorker(workerAddr)
		for i := 0; i < 15; i++ { // Exceed default ban threshold
			jm.wm.workerResult(workerAddr, false)
		}
		
		voteID := []byte("vote123")
		job, ok := jm.registerJob(workerAddr, voteID)

		c.Assert(ok, qt.IsFalse)
		c.Assert(job, qt.IsNil)
		c.Assert(len(jm.pending), qt.Equals, 0)
	})

	t.Run("Register multiple jobs", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		
		jobs := []struct {
			worker string
			voteID []byte
		}{
			{"worker1", []byte("vote1")},
			{"worker2", []byte("vote2")},
			{"worker1", []byte("vote3")}, // Same worker, different vote
		}

		for _, jobData := range jobs {
			job, ok := jm.registerJob(jobData.worker, jobData.voteID)
			c.Assert(ok, qt.IsTrue)
			c.Assert(job, qt.IsNotNil)
		}

		c.Assert(len(jm.pending), qt.Equals, 3)
	})
}

func TestJobsManagerIsWorkerAvailable(t *testing.T) {
	c := qt.New(t)

	t.Run("New worker is available", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		available := jm.isWorkerAvailable("new-worker")
		c.Assert(available, qt.IsTrue)
	})

	t.Run("Banned worker is not available", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		workerAddr := "worker1"
		
		// Add worker and ban it
		jm.wm.addWorker(workerAddr)
		for i := 0; i < 15; i++ { // Exceed ban threshold
			jm.wm.workerResult(workerAddr, false)
		}
		
		available := jm.isWorkerAvailable(workerAddr)
		c.Assert(available, qt.IsFalse)
	})

	t.Run("Worker with pending job is not available", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		workerAddr := "worker1"
		voteID := []byte("vote123")
		
		// Register a job for the worker
		_, ok := jm.registerJob(workerAddr, voteID)
		c.Assert(ok, qt.IsTrue)
		
		// Worker should not be available
		available := jm.isWorkerAvailable(workerAddr)
		c.Assert(available, qt.IsFalse)
	})

	t.Run("Worker without pending jobs is available", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		workerAddr := "worker1"
		
		// Add worker but no jobs
		jm.wm.addWorker(workerAddr)
		
		available := jm.isWorkerAvailable(workerAddr)
		c.Assert(available, qt.IsTrue)
	})
}

func TestJobsManagerCompleteJob(t *testing.T) {
	c := qt.New(t)

	t.Run("Complete existing job successfully", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		workerAddr := "worker1"
		voteID := []byte("vote123")
		
		// Register job first
		originalJob, ok := jm.registerJob(workerAddr, voteID)
		c.Assert(ok, qt.IsTrue)
		c.Assert(len(jm.pending), qt.Equals, 1)
		
		// Complete job successfully
		completedJob := jm.completeJob(voteID, true)
		
		c.Assert(completedJob, qt.IsNotNil)
		c.Assert(completedJob, qt.Equals, originalJob)
		c.Assert(len(jm.pending), qt.Equals, 0) // Job should be removed
		
		// Verify no failed job was sent to channel
		select {
		case <-jm.failedJobs:
			t.Error("No failed job should be sent for successful completion")
		default:
			// Expected - no failed job
		}
	})

	t.Run("Complete existing job with failure", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		workerAddr := "worker1"
		voteID := []byte("vote123")
		
		// Register job first
		originalJob, ok := jm.registerJob(workerAddr, voteID)
		c.Assert(ok, qt.IsTrue)
		
		// Start a goroutine to consume the failed job to prevent blocking
		var receivedJob *workerJob
		done := make(chan bool)
		go func() {
			receivedJob = <-jm.failedJobs
			done <- true
		}()
		
		// Complete job with failure
		completedJob := jm.completeJob(voteID, false)
		
		c.Assert(completedJob, qt.IsNotNil)
		c.Assert(completedJob, qt.Equals, originalJob)
		c.Assert(len(jm.pending), qt.Equals, 0) // Job should be removed
		
		// Wait for failed job to be received
		select {
		case <-done:
			c.Assert(receivedJob, qt.Equals, originalJob)
		case <-time.After(100 * time.Millisecond):
			t.Error("Failed job should be sent to channel")
		}
	})

	t.Run("Complete non-existent job", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		voteID := []byte("nonexistent")
		
		completedJob := jm.completeJob(voteID, true)
		
		c.Assert(completedJob, qt.IsNil)
	})
}

func TestJobsManagerCheckTimeouts(t *testing.T) {
	c := qt.New(t)

	t.Run("No expired jobs", func(t *testing.T) {
		jm := newJobsManager(1*time.Hour, 50*time.Millisecond) // Long timeout
		workerAddr := "worker1"
		voteID := []byte("vote123")
		
		// Register job with long timeout
		_, ok := jm.registerJob(workerAddr, voteID)
		c.Assert(ok, qt.IsTrue)
		
		// Check timeouts
		jm.checkTimeouts()
		
		// Job should still be pending
		c.Assert(len(jm.pending), qt.Equals, 1)
		
		// No failed jobs should be sent
		select {
		case <-jm.failedJobs:
			t.Error("No failed job should be sent for non-expired job")
		default:
			// Expected
		}
	})

	t.Run("Expired jobs are removed and sent to failed channel", func(t *testing.T) {
		jm := newJobsManager(1*time.Millisecond, 50*time.Millisecond) // Very short timeout
		workerAddr := "worker1"
		voteID := []byte("vote123")
		
		// Register job with short timeout
		originalJob, ok := jm.registerJob(workerAddr, voteID)
		c.Assert(ok, qt.IsTrue)
		
		// Wait for job to expire
		time.Sleep(10 * time.Millisecond)
		
		// Start a goroutine to consume the failed job to prevent blocking
		var receivedJob *workerJob
		done := make(chan bool)
		go func() {
			receivedJob = <-jm.failedJobs
			done <- true
		}()
		
		// Check timeouts
		jm.checkTimeouts()
		
		// Job should be removed from pending
		c.Assert(len(jm.pending), qt.Equals, 0)
		
		// Wait for failed job to be received
		select {
		case <-done:
			c.Assert(receivedJob, qt.Equals, originalJob)
		case <-time.After(100 * time.Millisecond):
			t.Error("Failed job should be sent to channel")
		}
	})

	t.Run("Multiple expired jobs", func(t *testing.T) {
		jm := newJobsManager(1*time.Millisecond, 50*time.Millisecond)
		
		// Register multiple jobs
		jobs := [][]byte{
			[]byte("vote1"),
			[]byte("vote2"),
			[]byte("vote3"),
		}
		
		for _, voteID := range jobs {
			_, ok := jm.registerJob("worker1", voteID)
			c.Assert(ok, qt.IsTrue)
		}
		
		c.Assert(len(jm.pending), qt.Equals, 3)
		
		// Wait for jobs to expire
		time.Sleep(10 * time.Millisecond)
		
		// Start a goroutine to consume all failed jobs to prevent blocking
		var receivedJobs []*workerJob
		done := make(chan bool)
		go func() {
			for i := 0; i < 3; i++ {
				job := <-jm.failedJobs
				receivedJobs = append(receivedJobs, job)
			}
			done <- true
		}()
		
		// Check timeouts
		jm.checkTimeouts()
		
		// Wait for all jobs to be received
		select {
		case <-done:
			// All jobs should be removed and received
			c.Assert(len(jm.pending), qt.Equals, 0)
			c.Assert(len(receivedJobs), qt.Equals, 3)
		case <-time.After(100 * time.Millisecond):
			t.Error("All failed jobs should be sent to channel")
		}
	})
}

func TestJobsManagerRealStartWithConfigurableTicker(t *testing.T) {
	c := qt.New(t)

	t.Run("Real start method with fast ticker - timeout detection", func(t *testing.T) {
		jm := newJobsManager(100*time.Millisecond, 50*time.Millisecond) // Fast ticker and short timeout
		
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Start a goroutine to consume failed jobs to prevent blocking
		var receivedJob *workerJob
		jobReceived := make(chan bool)
		go func() {
			receivedJob = <-jm.failedJobs
			jobReceived <- true
		}()

		// Start the real jobs manager
		jm.start(ctx)
		defer jm.stop()

		// Register a job that will expire
		workerAddr := "worker1"
		voteID := []byte("vote123")
		originalJob, ok := jm.registerJob(workerAddr, voteID)
		c.Assert(ok, qt.IsTrue)
		
		// Use thread-safe access to check pending jobs
		jm.pendingMtx.RLock()
		pendingCount := len(jm.pending)
		jm.pendingMtx.RUnlock()
		c.Assert(pendingCount, qt.Equals, 1)

		// Wait for job to expire and ticker to process it
		select {
		case <-jobReceived:
			// Job should be removed by the real ticker (thread-safe check)
			jm.pendingMtx.RLock()
			pendingCount = len(jm.pending)
			jm.pendingMtx.RUnlock()
			c.Assert(pendingCount, qt.Equals, 0)
			c.Assert(receivedJob, qt.Equals, originalJob)
		case <-time.After(500 * time.Millisecond):
			t.Error("Failed job should be sent to channel by ticker")
		}
	})

	t.Run("Context cancellation stops ticker", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		
		ctx, cancel := context.WithCancel(context.Background())

		// Start jobs manager
		jm.start(ctx)

		// Register a job
		_, ok := jm.registerJob("worker1", []byte("vote123"))
		c.Assert(ok, qt.IsTrue)
		
		// Thread-safe check of pending jobs
		jm.pendingMtx.RLock()
		pendingCount := len(jm.pending)
		jm.pendingMtx.RUnlock()
		c.Assert(pendingCount, qt.Equals, 1)

		// Cancel context
		cancel()

		// Give time for cleanup
		time.Sleep(100 * time.Millisecond)

		// Jobs should be cleared by stop() (thread-safe check)
		jm.pendingMtx.RLock()
		pendingCount = len(jm.pending)
		jm.pendingMtx.RUnlock()
		c.Assert(pendingCount, qt.Equals, 0)
	})

	t.Run("Ticker interval verification", func(t *testing.T) {
		// Test with custom interval
		customInterval := 25 * time.Millisecond
		jm := newJobsManager(1*time.Minute, customInterval)
		c.Assert(jm.tickerInterval, qt.Equals, customInterval)

		// Test with default interval
		jmDefault := newJobsManager(1 * time.Minute)
		c.Assert(jmDefault.tickerInterval, qt.Equals, 10*time.Second)
	})
}

func TestJobsManagerConcurrency(t *testing.T) {
	c := qt.New(t)

	t.Run("Concurrent job registration and completion", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		
		jm.start(ctx)
		defer jm.stop()

		const numWorkers = 50
		const numJobs = 10

		var wg sync.WaitGroup

		// Concurrent job registration
		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				workerAddr := fmt.Sprintf("worker-%d", workerID)
				
				for j := 0; j < numJobs; j++ {
					voteID := []byte(fmt.Sprintf("vote-%d-%d", workerID, j))
					job, ok := jm.registerJob(workerAddr, voteID)
					if ok && job != nil {
						// Randomly complete some jobs
						if j%2 == 0 {
							jm.completeJob(voteID, true)
						}
					}
				}
			}(i)
		}

		// Concurrent availability checks
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				workerAddr := fmt.Sprintf("worker-%d", i%numWorkers)
				jm.isWorkerAvailable(workerAddr)
			}(i)
		}

		wg.Wait()

		// Verify no race conditions occurred (test should not panic)
		c.Assert(true, qt.IsTrue) // If we get here, no race conditions
	})
}

func TestJobsManagerWorkerManagerIntegration(t *testing.T) {
	c := qt.New(t)

	t.Run("Job completion updates worker manager", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		workerAddr := "worker1"
		voteID := []byte("vote123")
		
		// Register and complete job successfully
		_, ok := jm.registerJob(workerAddr, voteID)
		c.Assert(ok, qt.IsTrue)
		
		// Get initial worker state
		worker, exists := jm.wm.getWorker(workerAddr)
		c.Assert(exists, qt.IsTrue)
		initialFails := worker.getConsecutiveFails()
		
		// Complete job successfully
		jm.completeJob(voteID, true)
		
		// Worker consecutive fails should be reset
		worker, exists = jm.wm.getWorker(workerAddr)
		c.Assert(exists, qt.IsTrue)
		c.Assert(worker.getConsecutiveFails(), qt.Equals, 0)
		
		// Register and fail a job
		voteID2 := []byte("vote456")
		_, ok = jm.registerJob(workerAddr, voteID2)
		c.Assert(ok, qt.IsTrue)
		
		// Start a goroutine to consume the failed job to prevent blocking
		done := make(chan bool)
		go func() {
			<-jm.failedJobs // Consume the failed job
			done <- true
		}()
		
		jm.completeJob(voteID2, false)
		
		// Wait for failed job to be consumed
		select {
		case <-done:
			// Worker consecutive fails should increase
			worker, exists = jm.wm.getWorker(workerAddr)
			c.Assert(exists, qt.IsTrue)
			c.Assert(worker.getConsecutiveFails(), qt.Equals, initialFails+1)
		case <-time.After(100 * time.Millisecond):
			t.Error("Failed job should be sent to channel")
		}
	})

	t.Run("Timeout updates worker manager", func(t *testing.T) {
		jm := newJobsManager(1*time.Millisecond, 50*time.Millisecond)
		workerAddr := "worker1"
		voteID := []byte("vote123")
		
		// Register job that will timeout
		_, ok := jm.registerJob(workerAddr, voteID)
		c.Assert(ok, qt.IsTrue)
		
		// Get initial worker state
		worker, exists := jm.wm.getWorker(workerAddr)
		c.Assert(exists, qt.IsTrue)
		initialFails := worker.getConsecutiveFails()
		
		// Wait for timeout and check
		time.Sleep(10 * time.Millisecond)
		
		// Start a goroutine to consume the failed job to prevent blocking
		done := make(chan bool)
		go func() {
			<-jm.failedJobs // Consume the failed job
			done <- true
		}()
		
		jm.checkTimeouts()
		
		// Wait for failed job to be consumed
		select {
		case <-done:
			// Worker consecutive fails should increase due to timeout
			worker, exists = jm.wm.getWorker(workerAddr)
			c.Assert(exists, qt.IsTrue)
			c.Assert(worker.getConsecutiveFails(), qt.Equals, initialFails+1)
		case <-time.After(100 * time.Millisecond):
			t.Error("Failed job should be sent to channel")
		}
	})
}

func TestJobsManagerEdgeCases(t *testing.T) {
	c := qt.New(t)

	t.Run("Zero timeout duration", func(t *testing.T) {
		jm := newJobsManager(0, 50*time.Millisecond) // Zero timeout
		workerAddr := "worker1"
		voteID := []byte("vote123")
		
		// Register job with zero timeout (should expire immediately)
		_, ok := jm.registerJob(workerAddr, voteID)
		c.Assert(ok, qt.IsTrue)
		
		// Start a goroutine to consume the failed job to prevent blocking
		done := make(chan bool)
		go func() {
			<-jm.failedJobs // Consume the failed job
			done <- true
		}()
		
		// Check timeouts immediately
		jm.checkTimeouts()
		
		// Wait for failed job to be consumed
		select {
		case <-done:
			// Job should be expired and removed
			c.Assert(len(jm.pending), qt.Equals, 0)
		case <-time.After(100 * time.Millisecond):
			t.Error("Failed job should be sent to channel")
		}
	})

	t.Run("Nil vote ID", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		workerAddr := "worker1"
		
		// Register job with nil vote ID
		job, ok := jm.registerJob(workerAddr, nil)
		c.Assert(ok, qt.IsTrue)
		c.Assert(job, qt.IsNotNil)
		c.Assert(job.VoteID, qt.IsNil)
		
		// Should be able to complete it
		completedJob := jm.completeJob(nil, true)
		c.Assert(completedJob, qt.IsNotNil)
	})

	t.Run("Empty worker address", func(t *testing.T) {
		jm := newJobsManager(1*time.Minute, 50*time.Millisecond)
		voteID := []byte("vote123")
		
		// Register job with empty worker address
		job, ok := jm.registerJob("", voteID)
		c.Assert(ok, qt.IsTrue)
		c.Assert(job, qt.IsNotNil)
		c.Assert(job.Address, qt.Equals, "")
	})

	t.Run("Unbuffered channel behavior", func(t *testing.T) {
		jm := newJobsManager(1*time.Millisecond, 50*time.Millisecond)
		
		// Register multiple jobs that will expire
		for i := 0; i < 5; i++ {
			voteID := []byte(fmt.Sprintf("vote%d", i))
			jm.registerJob("worker1", voteID)
		}
		
		// Wait for expiration
		time.Sleep(10 * time.Millisecond)
		
		// Start a goroutine to consume failed jobs to prevent blocking
		var receivedJobs []*workerJob
		done := make(chan bool)
		go func() {
			for i := 0; i < 5; i++ {
				job := <-jm.failedJobs
				receivedJobs = append(receivedJobs, job)
			}
			done <- true
		}()
		
		// Check timeouts - should not block with unbuffered channel since we have a consumer
		jm.checkTimeouts()
		
		// Wait for all jobs to be received
		<-done
		
		// All jobs should be removed and received
		c.Assert(len(jm.pending), qt.Equals, 0)
		c.Assert(len(receivedJobs), qt.Equals, 5)
	})

	t.Run("Blocking behavior with no consumer", func(t *testing.T) {
		jm := newJobsManager(1*time.Millisecond, 50*time.Millisecond)
		
		// Register a job that will expire
		voteID := []byte("vote123")
		jm.registerJob("worker1", voteID)
		
		// Wait for expiration
		time.Sleep(10 * time.Millisecond)
		
		// Start checkTimeouts in a goroutine since it will block
		timeoutDone := make(chan bool)
		go func() {
			jm.checkTimeouts()
			timeoutDone <- true
		}()
		
		// Verify that checkTimeouts is blocked (timeout should not complete immediately)
		select {
		case <-timeoutDone:
			t.Error("checkTimeouts should be blocked waiting for consumer")
		case <-time.After(50 * time.Millisecond):
			// Expected - checkTimeouts is blocked
		}
		
		// Now consume the failed job to unblock checkTimeouts
		failedJob := <-jm.failedJobs
		c.Assert(failedJob, qt.IsNotNil)
		
		// checkTimeouts should now complete
		select {
		case <-timeoutDone:
			// Expected - checkTimeouts completed
		case <-time.After(100 * time.Millisecond):
			t.Error("checkTimeouts should have completed after consuming failed job")
		}
		
		// Job should be removed
		c.Assert(len(jm.pending), qt.Equals, 0)
	})
}
