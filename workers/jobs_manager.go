package workers

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

var (
	ErrWorkerNotFound = fmt.Errorf("worker not found")
	ErrWorkerBanned   = fmt.Errorf("worker is banned")
)

// defaultTickerInterval defines the default interval for the ticker that
// checks for job timeouts. It is set to 10 seconds, but can be overridden
// when creating a new JobsManager instance.
const defaultTickerInterval = 10 * time.Second

// WorkerJob represents a job assigned to a worker. It contains the vote ID,
// worker address, timestamp, and expiration time.
type WorkerJob struct {
	VoteID     types.HexBytes
	Address    string
	Timestamp  time.Time
	Expiration time.Time // When the job should expire
}

// JobsManager manages worker jobs, including job registration, completion,
// and timeout handling. It also interacts with the worker manager to track
// worker availability and job results.
type JobsManager struct {
	ctx            context.Context
	cancel         context.CancelFunc
	pendingMtx     sync.RWMutex
	pending        map[string]*WorkerJob
	tickerInterval time.Duration // Configurable ticker interval for timeout checking
	closeOnce      sync.Once     // Ensures channel is closed only once

	FailedJobs    chan *WorkerJob // Channel to handle failed jobs
	JobTimeout    time.Duration   // Duration after which a job is considered timed out
	WorkerManager *WorkerManager  // Reference to the worker manager for job tracking
}

// NewJobsManager creates a new jobs manager with the specified job timeout
// and ticker interval. If no ticker interval is provided, it defaults to 10
// seconds. It initializes an internal worker manager with default ban rules.
func NewJobsManager(storage *storage.Storage, jobTimeout time.Duration, banRules *WorkerBanRules, tickerInterval ...time.Duration) *JobsManager {
	interval := defaultTickerInterval
	if len(tickerInterval) > 0 {
		interval = tickerInterval[0]
	}
	return &JobsManager{
		pending:        make(map[string]*WorkerJob),
		WorkerManager:  NewWorkerManager(storage, banRules),
		FailedJobs:     make(chan *WorkerJob), // Unbuffered channel for failed jobs
		JobTimeout:     jobTimeout,
		tickerInterval: interval,
	}
}

// Start initializes the jobs manager, starts the worker manager, and begins
// a goroutine to periodically check for job timeouts. It uses a context to
// manage the lifecycle of the jobs manager, allowing it to be stopped
// gracefully.
func (jm *JobsManager) Start(ctx context.Context) {
	jm.ctx, jm.cancel = context.WithCancel(ctx)
	jm.WorkerManager.Start(ctx)

	go func() {
		ticker := time.NewTicker(jm.tickerInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				jm.Stop() // Stop the jobs manager when context is done
				return
			case <-ticker.C:
				jm.checkTimeouts()
			}
		}
	}()
	log.Infow("jobs manager started",
		"jobTimeout", jm.JobTimeout.String(),
		"tickerInterval", jm.tickerInterval.String())
}

// Stop gracefully stops the jobs manager, clearing all pending jobs and
// stopping the worker manager. It also ensures that the failed jobs channel
// is closed only once using sync.Once. This prevents any potential concurrent
// write to a closed channel panic.
func (jm *JobsManager) Stop() {
	if jm.cancel != nil {
		jm.cancel()
	}
	jm.pendingMtx.Lock()
	defer jm.pendingMtx.Unlock()
	jm.pending = make(map[string]*WorkerJob) // Clear all pending jobs
	jm.WorkerManager.Stop()                  // Stop the worker manager

	// Close the failed jobs channel safely using sync.Once
	jm.closeOnce.Do(func() {
		close(jm.FailedJobs)
	})
}

// checkTimeouts checks for any pending jobs that have expired based on their
// expiration time. If a job has expired, it notifies the worker manager of
// the timeout and sends the job to the failed jobs channel. It also removes
// the expired job from the pending jobs map. This function is called
// periodically by a ticker to ensure timely handling of job timeouts.
func (jm *JobsManager) checkTimeouts() {
	jm.pendingMtx.Lock()
	defer jm.pendingMtx.Unlock()

	now := time.Now()
	for key, job := range jm.pending {
		if now.After(job.Expiration) {
			log.Debugf("job with vote ID %x has expired", job.VoteID)
			if err := jm.WorkerManager.WorkerResult(job.Address, false); err != nil {
				log.Warnw("failed to notify worker manager for job",
					"voteID", job.VoteID.String(),
					"error", err)
			}
			jm.FailedJobs <- job    // Send to failed jobs channel (blocking)
			delete(jm.pending, key) // Remove expired job
		}
	}
}

// IsWorkerAvailable checks if a worker is available for a new job. It verifies
// if the worker exists, is not banned, and does not have any pending jobs.
func (jm *JobsManager) IsWorkerAvailable(workerAddr string) (bool, error) {
	worker, ok := jm.WorkerManager.GetWorker(workerAddr)
	if !ok {
		return true, nil // Worker does not exist, consider available
	}
	// Check if worker is banned
	if worker.IsBanned(jm.WorkerManager.rules) {
		return false, fmt.Errorf("worker banned") // Worker is banned
	}
	// Check if worker has pending jobs
	jm.pendingMtx.RLock()
	defer jm.pendingMtx.RUnlock()
	for _, job := range jm.pending {
		if job.Address == worker.Address {
			return false, fmt.Errorf("worker busy") // Worker has pending jobs
		}
	}
	return true, nil // Worker is available
}

// RegisterJob registers a new job for a worker. It checks if the worker is
// available and not banned. If the worker is valid, it creates a new job
// with the provided vote ID, assigns it to the worker, and sets an expiration
// time for the job. The job is then added to the pending jobs map. If the
// worker is banned returns nil.
func (jm *JobsManager) RegisterJob(workerAddr string, voteID types.HexBytes) (*WorkerJob, error) {
	jm.pendingMtx.Lock()
	defer jm.pendingMtx.Unlock()
	// Check if worker exists in the worker manager
	worker, ok := jm.WorkerManager.GetWorker(workerAddr)
	if !ok {
		log.Warnw("worker not found", "workerAddr", workerAddr, "voteID", voteID.String())
		return nil, ErrWorkerNotFound
	}
	// Check if worker is available
	if worker.IsBanned(jm.WorkerManager.rules) {
		log.Warnw("worker is banned, cannot register job", "workerAddr", workerAddr, "voteID", voteID.String())
		return nil, ErrWorkerBanned // Worker is banned
	}
	job := &WorkerJob{
		VoteID:     voteID,
		Address:    worker.Address,
		Timestamp:  time.Now(),
		Expiration: time.Now().Add(jm.JobTimeout), // Default expiration
	}
	jm.pending[hex.EncodeToString(voteID)] = job
	log.Debugw("job registered",
		"voteID", voteID.String(),
		"workerAddr", workerAddr,
		"expiration", job.Expiration.String())
	return job, nil
}

// CompleteJob marks a job as completed, either successfully or with failure.
// It looks up the job by its vote ID, updates the worker manager with the
// result, and removes the job from the pending jobs map. If the job is not
// found, it logs a warning and returns nil. If the job is marked as failed,
// it sends the job to the failed jobs channel for further processing. This
// function is called when a worker completes a job, either successfully or
// with failure.
func (jm *JobsManager) CompleteJob(voteID types.HexBytes, success bool) *WorkerJob {
	jm.pendingMtx.Lock()
	defer jm.pendingMtx.Unlock()
	// Look up the job by vote ID
	job, exists := jm.pending[hex.EncodeToString(voteID)]
	if !exists {
		log.Warnw("job not found by voteID",
			"voteID", voteID.String(),
			"success", success)

		return nil // Job not found
	}
	if !success {
		jm.FailedJobs <- job // Send to failed jobs channel (blocking)
	}
	// Notify worker manager
	if err := jm.WorkerManager.WorkerResult(job.Address, success); err != nil {
		log.Warnw("failed to notify worker manager for job",
			"voteID", voteID.String(),
			"success", success)
	}
	delete(jm.pending, hex.EncodeToString(voteID)) // Remove the job from pending
	log.Debugw("job completed", "voteID", voteID.String(), "success", success)
	return job
}
