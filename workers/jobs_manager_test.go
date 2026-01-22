package workers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

const (
	testWorkerAddr = "0x123456789"
	testWorkerName = "TestWorker1"
)

var testVoteID = testutil.RandomVoteID()

func storageForTest(t *testing.T) *storage.Storage {
	c := qt.New(t)
	tempDir := t.TempDir()
	t.Cleanup(func() {
		// remove the temporary directory after the test
		_ = os.RemoveAll(tempDir)
	})
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)
	return storage.New(db)
}

func TestNewJobsManager(t *testing.T) {
	c := qt.New(t)

	t.Run("Default configuration", func(t *testing.T) {
		jobTimeout := 5 * time.Minute

		jm := NewJobsManager(storageForTest(t), jobTimeout, nil)

		c.Assert(jm, qt.IsNotNil)
		c.Assert(jm.JobTimeout, qt.Equals, jobTimeout)
		c.Assert(jm.tickerInterval, qt.Equals, 10*time.Second) // Default interval
		c.Assert(jm.pending, qt.IsNotNil)
		c.Assert(len(jm.pending), qt.Equals, 0)
		c.Assert(jm.FailedJobs, qt.IsNotNil)
		c.Assert(jm.WorkerManager, qt.IsNotNil)
		c.Assert(jm.ctx, qt.IsNil) // Should be nil until start() is called
		c.Assert(jm.cancel, qt.IsNil)
	})

	t.Run("Custom ticker interval", func(t *testing.T) {
		jobTimeout := 2 * time.Minute
		tickerInterval := 100 * time.Millisecond
		jm := NewJobsManager(storageForTest(t), jobTimeout, nil, tickerInterval)

		c.Assert(jm.JobTimeout, qt.Equals, jobTimeout)
		c.Assert(jm.tickerInterval, qt.Equals, tickerInterval)
	})
}

func TestJobsManagerStartStop(t *testing.T) {
	c := qt.New(t)

	jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test start
	jm.Start(ctx)
	c.Assert(jm.ctx, qt.IsNotNil)
	c.Assert(jm.cancel, qt.IsNotNil)

	// Test stop
	jm.Stop()

	// Verify cleanup
	c.Assert(len(jm.pending), qt.Equals, 0)

	// Verify channel is closed by trying to receive (should not block)
	select {
	case _, ok := <-jm.FailedJobs:
		c.Assert(ok, qt.IsFalse) // Channel should be closed
	default:
		t.Error("Expected channel to be closed")
	}
}

func TestJobsManagerRegisterJob(t *testing.T) {
	c := qt.New(t)

	t.Run("Register job for new worker", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)

		// Add worker first
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)
		// Register job for the worker
		job, err := jm.RegisterJob(testWorkerAddr, testVoteID)

		c.Assert(err, qt.IsNil)
		c.Assert(job, qt.IsNotNil)
		c.Assert(job.VoteID.String(), qt.DeepEquals, testVoteID.String())
		c.Assert(job.Address, qt.Equals, testWorkerAddr)
		c.Assert(job.Timestamp.IsZero(), qt.IsFalse)
		c.Assert(job.Expiration.After(job.Timestamp), qt.IsTrue)

		// Verify job is in pending map
		c.Assert(len(jm.pending), qt.Equals, 1)
		storedJob, exists := jm.pending[testVoteID]
		c.Assert(exists, qt.IsTrue)
		c.Assert(storedJob, qt.Equals, job)
	})

	t.Run("Register job for existing worker", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)

		// Add worker first
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)

		job, err := jm.RegisterJob(testWorkerAddr, testVoteID)
		c.Assert(err, qt.IsNil)
		c.Assert(job, qt.IsNotNil)
		c.Assert(job.Address, qt.Equals, testWorkerAddr)
	})

	t.Run("Register job for banned worker", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)

		// Add worker and make it fail to get banned
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)
		for range 15 { // Exceed default ban threshold
			err := jm.WorkerManager.WorkerResult(testWorkerAddr, false)
			c.Assert(err, qt.IsNil)
		}

		job, err := jm.RegisterJob(testWorkerAddr, testVoteID)
		c.Assert(err, qt.ErrorIs, ErrWorkerBanned)

		c.Assert(job, qt.IsNil)
		c.Assert(len(jm.pending), qt.Equals, 0)
	})

	t.Run("Register multiple jobs", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)

		jobs := []struct {
			worker string
			voteID types.VoteID
		}{
			{"worker1", testutil.RandomVoteID()},
			{"worker2", testutil.RandomVoteID()},
			{"worker1", testutil.RandomVoteID()}, // Same worker, different vote
		}

		for _, jobData := range jobs {
			jm.WorkerManager.AddWorker(jobData.worker, fmt.Sprintf("Worker %s", jobData.worker))
			job, err := jm.RegisterJob(jobData.worker, jobData.voteID)
			c.Assert(err, qt.IsNil)
			c.Assert(job, qt.IsNotNil)
		}

		c.Assert(len(jm.pending), qt.Equals, 3)
	})
}

func TestJobsManagerIsWorkerAvailable(t *testing.T) {
	c := qt.New(t)

	t.Run("New worker is available", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)
		available, err := jm.IsWorkerAvailable("new-worker")
		c.Assert(err, qt.IsNil)
		c.Assert(available, qt.IsTrue)
	})

	t.Run("Banned worker is not available", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)

		// Add worker and ban it
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)
		for range 15 { // Exceed ban threshold
			err := jm.WorkerManager.WorkerResult(testWorkerAddr, false)
			c.Assert(err, qt.IsNil)
		}

		available, err := jm.IsWorkerAvailable(testWorkerAddr)
		c.Assert(available, qt.IsFalse)
		c.Assert(err, qt.ErrorIs, ErrWorkerBanned)
	})

	t.Run("Worker with pending job is not available", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)

		// Add worker first
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)

		// Register a job for the worker
		_, err := jm.RegisterJob(testWorkerAddr, testVoteID)
		c.Assert(err, qt.IsNil)

		// Worker should not be available
		available, err := jm.IsWorkerAvailable(testWorkerAddr)
		c.Assert(available, qt.IsFalse)
		c.Assert(err, qt.ErrorIs, ErrWorkerBusy)
	})

	t.Run("Worker without pending jobs is available", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)

		// Add worker but no jobs
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)

		available, err := jm.IsWorkerAvailable(testWorkerAddr)
		c.Assert(available, qt.IsTrue)
		c.Assert(err, qt.IsNil)
	})
}

func TestJobsManagerCompleteJob(t *testing.T) {
	c := qt.New(t)

	t.Run("Complete existing job successfully", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)

		// Add worker first
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)

		// Register job first
		originalJob, err := jm.RegisterJob(testWorkerAddr, testVoteID)
		c.Assert(err, qt.IsNil)
		c.Assert(len(jm.pending), qt.Equals, 1)

		// Complete job successfully
		completedJob := jm.CompleteJob(testVoteID, true)

		c.Assert(completedJob, qt.IsNotNil)
		c.Assert(completedJob, qt.Equals, originalJob)
		c.Assert(len(jm.pending), qt.Equals, 0) // Job should be removed

		// Verify no failed job was sent to channel
		select {
		case <-jm.FailedJobs:
			t.Error("No failed job should be sent for successful completion")
		default:
			// Expected - no failed job
		}
	})

	t.Run("Complete existing job with failure", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)

		// Add worker first
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)

		// Register job first
		originalJob, err := jm.RegisterJob(testWorkerAddr, testVoteID)
		c.Assert(err, qt.IsNil)

		// Start a goroutine to consume the failed job to prevent blocking
		var receivedJob *WorkerJob
		done := make(chan bool)
		go func() {
			receivedJob = <-jm.FailedJobs
			done <- true
		}()

		// Complete job with failure
		completedJob := jm.CompleteJob(testVoteID, false)

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
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)
		voteID := testutil.RandomVoteID()

		completedJob := jm.CompleteJob(voteID, true)

		c.Assert(completedJob, qt.IsNil)
	})
}

func TestJobsManagerCheckTimeouts(t *testing.T) {
	c := qt.New(t)

	t.Run("No expired jobs", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Hour, nil, 50*time.Millisecond) // Long timeout

		// Add worker first
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)

		// Register job with long timeout
		_, err := jm.RegisterJob(testWorkerAddr, testVoteID)
		c.Assert(err, qt.IsNil)

		// Check timeouts
		jm.checkTimeouts()

		// Job should still be pending
		c.Assert(len(jm.pending), qt.Equals, 1)

		// No failed jobs should be sent
		select {
		case <-jm.FailedJobs:
			t.Error("No failed job should be sent for non-expired job")
		default:
			// Expected
		}
	})

	t.Run("Expired jobs are removed and sent to failed channel", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Millisecond, nil, 50*time.Millisecond) // Very short timeout

		// Add worker first
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)

		// Register job with short timeout
		originalJob, err := jm.RegisterJob(testWorkerAddr, testVoteID)
		c.Assert(err, qt.IsNil)

		// Wait for job to expire
		time.Sleep(10 * time.Millisecond)

		// Start a goroutine to consume the failed job to prevent blocking
		var receivedJob *WorkerJob
		done := make(chan bool)
		go func() {
			receivedJob = <-jm.FailedJobs
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
		jm := NewJobsManager(storageForTest(t), 1*time.Millisecond, nil, 50*time.Millisecond)

		// Register multiple jobs
		for _, testVoteID := range testutil.RandomVoteIDs(3) {
			// Add worker first
			jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)
			_, err := jm.RegisterJob(testWorkerAddr, testVoteID)
			c.Assert(err, qt.IsNil)
		}

		c.Assert(len(jm.pending), qt.Equals, 3)

		// Wait for jobs to expire
		time.Sleep(10 * time.Millisecond)

		// Start a goroutine to consume all failed jobs to prevent blocking
		var receivedJobs []*WorkerJob
		done := make(chan bool)
		go func() {
			for range 3 {
				job := <-jm.FailedJobs
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
		jm := NewJobsManager(storageForTest(t), 100*time.Millisecond, nil, 50*time.Millisecond) // Fast ticker and short timeout

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Start a goroutine to consume failed jobs to prevent blocking
		var receivedJob *WorkerJob
		jobReceived := make(chan bool)
		go func() {
			receivedJob = <-jm.FailedJobs
			jobReceived <- true
		}()

		// Start the real jobs manager
		jm.Start(ctx)
		defer jm.Stop()

		// Add worker first
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)

		// Register a job that will expire
		originalJob, err := jm.RegisterJob(testWorkerAddr, testVoteID)
		c.Assert(err, qt.IsNil)

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
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)

		ctx, cancel := context.WithCancel(context.Background())

		// Start jobs manager
		jm.Start(ctx)

		// Add worker first
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)

		// Register a job
		_, err := jm.RegisterJob(testWorkerAddr, testVoteID)
		c.Assert(err, qt.IsNil)

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
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, customInterval)
		c.Assert(jm.tickerInterval, qt.Equals, customInterval)

		// Test with default interval
		jmDefault := NewJobsManager(storageForTest(t), 1*time.Minute, nil)
		c.Assert(jmDefault.tickerInterval, qt.Equals, 10*time.Second)
	})
}

func TestJobsManagerConcurrency(t *testing.T) {
	c := qt.New(t)

	t.Run("Concurrent job registration and completion", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		jm.Start(ctx)
		defer jm.Stop()

		const numWorkers = 50
		const numJobs = 10

		var wg sync.WaitGroup

		// Concurrent job registration
		for i := range numWorkers {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				testWorkerAddr := fmt.Sprintf("worker-%d", workerID)

				for j := range numJobs {
					voteID := testutil.RandomVoteID()
					jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)
					job, err := jm.RegisterJob(testWorkerAddr, testVoteID)
					if err == nil && job != nil {
						// Randomly complete some jobs
						if j%2 == 0 {
							jm.CompleteJob(voteID, true)
						}
					}
				}
			}(i)
		}

		// Concurrent availability checks
		for i := range 20 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				testWorkerAddr := fmt.Sprintf("worker-%d", i%numWorkers)
				_, _ = jm.IsWorkerAvailable(testWorkerAddr)
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
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)

		// Add worker first
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)

		// Register and complete job successfully
		_, err := jm.RegisterJob(testWorkerAddr, testVoteID)
		c.Assert(err, qt.IsNil)

		// Get initial worker state
		worker, exists := jm.WorkerManager.GetWorker(testWorkerAddr)
		c.Assert(exists, qt.IsTrue)
		initialFails := worker.SetConsecutiveFails()

		// Complete job successfully
		jm.CompleteJob(testVoteID, true)

		// Worker consecutive fails should be reset
		worker, exists = jm.WorkerManager.GetWorker(testWorkerAddr)
		c.Assert(exists, qt.IsTrue)
		c.Assert(worker.SetConsecutiveFails(), qt.Equals, 0)

		// Register and fail a job
		voteID2 := testutil.RandomVoteID()
		_, err = jm.RegisterJob(testWorkerAddr, voteID2)
		c.Assert(err, qt.IsNil)

		// Start a goroutine to consume the failed job to prevent blocking
		done := make(chan bool)
		go func() {
			<-jm.FailedJobs // Consume the failed job
			done <- true
		}()

		jm.CompleteJob(voteID2, false)

		// Wait for failed job to be consumed
		select {
		case <-done:
			// Worker consecutive fails should increase
			worker, exists = jm.WorkerManager.GetWorker(testWorkerAddr)
			c.Assert(exists, qt.IsTrue)
			c.Assert(worker.SetConsecutiveFails(), qt.Equals, initialFails+1)
		case <-time.After(100 * time.Millisecond):
			t.Error("Failed job should be sent to channel")
		}
	})

	t.Run("Timeout updates worker manager", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Millisecond, nil, 50*time.Millisecond)

		// Add worker first
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)

		// Register job that will timeout
		_, err := jm.RegisterJob(testWorkerAddr, testVoteID)
		c.Assert(err, qt.IsNil)

		// Get initial worker state
		worker, exists := jm.WorkerManager.GetWorker(testWorkerAddr)
		c.Assert(exists, qt.IsTrue)
		initialFails := worker.SetConsecutiveFails()

		// Wait for timeout and check
		time.Sleep(10 * time.Millisecond)

		// Start a goroutine to consume the failed job to prevent blocking
		done := make(chan bool)
		go func() {
			<-jm.FailedJobs // Consume the failed job
			done <- true
		}()

		jm.checkTimeouts()

		// Wait for failed job to be consumed
		select {
		case <-done:
			// Worker consecutive fails should increase due to timeout
			worker, exists = jm.WorkerManager.GetWorker(testWorkerAddr)
			c.Assert(exists, qt.IsTrue)
			c.Assert(worker.SetConsecutiveFails(), qt.Equals, initialFails+1)
		case <-time.After(100 * time.Millisecond):
			t.Error("Failed job should be sent to channel")
		}
	})
}

func TestJobsManagerEdgeCases(t *testing.T) {
	c := qt.New(t)

	t.Run("Zero timeout duration", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 0, nil, 50*time.Millisecond) // Zero timeout

		// Add worker first
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)

		// Register job with zero timeout (should expire immediately)
		_, err := jm.RegisterJob(testWorkerAddr, testVoteID)
		c.Assert(err, qt.IsNil)

		// Start a goroutine to consume the failed job to prevent blocking
		done := make(chan bool)
		go func() {
			<-jm.FailedJobs // Consume the failed job
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
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)

		// Add worker first
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)

		// Register job with 0 vote ID
		job, err := jm.RegisterJob(testWorkerAddr, 0)
		c.Assert(err, qt.IsNil)
		c.Assert(job, qt.IsNotNil)
		c.Assert(job.VoteID.Uint64(), qt.Equals, uint64(0))

		// Should be able to complete it
		completedJob := jm.CompleteJob(0, true)
		c.Assert(completedJob, qt.IsNotNil)
	})

	t.Run("Empty worker address", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Minute, nil, 50*time.Millisecond)

		// Register job with empty worker address
		job, err := jm.RegisterJob("", testVoteID)
		c.Assert(err, qt.ErrorIs, ErrWorkerNotFound)
		c.Assert(job, qt.IsNil)
	})

	t.Run("Unbuffered channel behavior", func(t *testing.T) {
		jm := NewJobsManager(storageForTest(t), 1*time.Millisecond, nil, 50*time.Millisecond)

		// Register multiple jobs that will expire
		for range 5 {
			voteID := testutil.RandomVoteID()
			// Add worker first
			jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)
			_, err := jm.RegisterJob(testWorkerAddr, voteID)
			c.Assert(err, qt.IsNil)
		}

		// Wait for expiration
		time.Sleep(10 * time.Millisecond)

		// Start a goroutine to consume failed jobs to prevent blocking
		var receivedJobs []*WorkerJob
		done := make(chan bool)
		go func() {
			for range 5 {
				job := <-jm.FailedJobs
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
		jm := NewJobsManager(storageForTest(t), 1*time.Millisecond, nil, 50*time.Millisecond)

		// Add worker first
		jm.WorkerManager.AddWorker(testWorkerAddr, testWorkerName)

		// Register a job that will expire
		_, err := jm.RegisterJob(testWorkerAddr, testVoteID)
		c.Assert(err, qt.IsNil)

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
		failedJob := <-jm.FailedJobs
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
