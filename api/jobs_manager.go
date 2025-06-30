package api

import (
	"context"
	"encoding/hex"
	"sync"
	"time"

	"github.com/vocdoni/davinci-node/log"
)

// workerJob represents a job assigned to a worker. It contains the vote ID,
// worker address, timestamp, and expiration time.
type workerJob struct {
	VoteID     []byte
	Address    string
	Timestamp  time.Time
	Expiration time.Time // When the job should expire
}

// jobsManager manages worker jobs, including job registration, completion,
// and timeout handling. It also interacts with the worker manager to track
// worker availability and job results.
type jobsManager struct {
	pendingMtx     sync.RWMutex
	pending        map[string]*workerJob
	failedJobs     chan *workerJob // Channel to handle failed jobs
	jobTimeout     time.Duration   // Duration after which a job is considered timed out
	tickerInterval time.Duration   // Configurable ticker interval for timeout checking
	closeOnce      sync.Once       // Ensures channel is closed only once

	ctx    context.Context
	cancel context.CancelFunc
	wm     *workerManager // Reference to the worker manager for job tracking
}

// newJobsManager creates a new jobs manager with the specified job timeout
// and ticker interval. If no ticker interval is provided, it defaults to 10
// seconds. It initializes an internal worker manager with default ban rules.
func newJobsManager(jobTimeout time.Duration, tickerInterval ...time.Duration) *jobsManager {
	interval := 10 * time.Second // default production interval
	if len(tickerInterval) > 0 {
		interval = tickerInterval[0]
	}
	return &jobsManager{
		pending:        make(map[string]*workerJob),
		wm:             newWorkerManager(defaultBanRules),
		failedJobs:     make(chan *workerJob), // Unbuffered channel for failed jobs
		jobTimeout:     jobTimeout,
		tickerInterval: interval,
	}
}

// start initializes the jobs manager, starts the worker manager, and begins
// a goroutine to periodically check for job timeouts. It uses a context to
// manage the lifecycle of the jobs manager, allowing it to be stopped
// gracefully.
func (jm *jobsManager) start(ctx context.Context) {
	jm.ctx, jm.cancel = context.WithCancel(ctx)
	jm.wm.start(ctx)

	go func() {
		ticker := time.NewTicker(jm.tickerInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				jm.stop() // Stop the jobs manager when context is done
				return
			case <-ticker.C:
				jm.checkTimeouts()
			}
		}
	}()
	log.Info("Jobs manager started")
}

// stop gracefully stops the jobs manager, clearing all pending jobs and
// stopping the worker manager. It also ensures that the failed jobs channel
// is closed only once using sync.Once. This prevents any potential concurrent
// write to a closed channel panic.
func (jm *jobsManager) stop() {
	if jm.cancel != nil {
		jm.cancel()
	}
	jm.pendingMtx.Lock()
	defer jm.pendingMtx.Unlock()
	jm.pending = make(map[string]*workerJob) // Clear all pending jobs
	jm.wm.stop()                             // Stop the worker manager

	// Close the failed jobs channel safely using sync.Once
	jm.closeOnce.Do(func() {
		close(jm.failedJobs)
	})
}

// checkTimeouts checks for any pending jobs that have expired based on their
// expiration time. If a job has expired, it notifies the worker manager of
// the timeout and sends the job to the failed jobs channel. It also removes
// the expired job from the pending jobs map. This function is called
// periodically by a ticker to ensure timely handling of job timeouts.
func (jm *jobsManager) checkTimeouts() {
	jm.pendingMtx.Lock()
	defer jm.pendingMtx.Unlock()

	now := time.Now()
	for key, job := range jm.pending {
		if now.After(job.Expiration) {
			log.Debugf("Job with vote ID %s has expired", job.VoteID)
			jm.wm.workerResult(job.Address, false) // Notify worker manager of timeout
			jm.failedJobs <- job                   // Send to failed jobs channel (blocking)
			delete(jm.pending, key)                // Remove expired job
		}
	}
}

// isWorkerAvailable checks if a worker is available for a new job. It verifies
// if the worker exists, is not banned, and does not have any pending jobs.
func (jm *jobsManager) isWorkerAvailable(workerAddr string) bool {
	worker, ok := jm.wm.getWorker(workerAddr)
	if !ok {
		return true // Worker does not exist, consider available
	}
	// Check if worker is banned
	if worker.isBanned(jm.wm.rules) {
		log.Warnf("Worker %s is banned", workerAddr)
		return false // Worker is banned
	}
	// Check if worker has pending jobs
	jm.pendingMtx.RLock()
	defer jm.pendingMtx.RUnlock()
	for _, job := range jm.pending {
		if job.Address == worker.ID {
			log.Debugf("Worker %s has pending job for vote ID %s", workerAddr, job.VoteID)
			return false // Worker has pending jobs
		}
	}
	return true // Worker is available
}

// registerJob registers a new job for a worker. It checks if the worker is
// available and not banned. If the worker is valid, it creates a new job
// with the provided vote ID, assigns it to the worker, and sets an expiration
// time for the job. The job is then added to the pending jobs map. If the
// worker is banned returns nil.
func (jm *jobsManager) registerJob(workerAddr string, voteID []byte) (*workerJob, bool) {
	jm.pendingMtx.Lock()
	defer jm.pendingMtx.Unlock()
	// Check if worker exists in the worker manager
	worker, ok := jm.wm.getWorker(workerAddr)
	if !ok {
		worker = jm.wm.addWorker(workerAddr) // Add worker if not exists
	}
	// Check if worker is available
	if worker.isBanned(jm.wm.rules) {
		log.Warnf("Worker %s is banned, cannot register job for vote ID %s", workerAddr, voteID)
		return nil, false // Worker is banned
	}
	job := &workerJob{
		VoteID:     voteID,
		Address:    worker.ID,
		Timestamp:  time.Now(),
		Expiration: time.Now().Add(jm.jobTimeout), // Default expiration
	}
	jm.pending[hex.EncodeToString(voteID)] = job
	log.Debugf("Job registered: %s for worker %s", voteID, workerAddr)
	return job, true
}

// completeJob marks a job as completed, either successfully or with failure.
// It looks up the job by its vote ID, updates the worker manager with the
// result, and removes the job from the pending jobs map. If the job is not
// found, it logs a warning and returns nil. If the job is marked as failed,
// it sends the job to the failed jobs channel for further processing. This
// function is called when a worker completes a job, either successfully or
// with failure.
func (jm *jobsManager) completeJob(voteID []byte, success bool) *workerJob {
	jm.pendingMtx.Lock()
	defer jm.pendingMtx.Unlock()
	// Look up the job by vote ID
	job, exists := jm.pending[hex.EncodeToString(voteID)]
	if !exists {
		log.Warnf("Job with vote ID %s not found", voteID)
		return nil // Job not found
	}
	if !success {
		jm.failedJobs <- job // Send to failed jobs channel (blocking)
	}
	jm.wm.workerResult(job.Address, success)       // Notify worker manager
	delete(jm.pending, hex.EncodeToString(voteID)) // Remove the job from pending
	log.Debugf("Job completed: %s, success: %t", voteID, success)
	return job
}
