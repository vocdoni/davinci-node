package api

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vocdoni/davinci-node/log"
)

// banRules defines the rules for banning workers. It includes the duration
// for which a worker is banned and the maximum number of consecutive failed
// jobs before a worker is banned.
type banRules struct {
	timeout             time.Duration // Duration for which the worker is banned
	maxConsecutiveFails int           // Maximum consecutive failed jobs before banning
}

// defaultBanRules provides the default ban rules for workers
var defaultBanRules = banRules{
	timeout:             3 * time.Minute, // Ban for 3 minutes
	maxConsecutiveFails: 10,              // 3 consecutive failed jobs
}

// worker represents a worker that processes jobs
type worker struct {
	ID               string
	consecutiveFails int64 // atomic counter
	bannedUntilNanos int64 // atomic Unix nanoseconds, 0 = not banned
}

// isBanned checks if the worker is banned based on the provided rules
func (w *worker) isBanned(rules banRules) bool {
	consecutiveFails := atomic.LoadInt64(&w.consecutiveFails)
	if consecutiveFails > int64(rules.maxConsecutiveFails) {
		return true
	}

	// Check time-based ban
	bannedUntil := atomic.LoadInt64(&w.bannedUntilNanos)
	if bannedUntil == 0 {
		return false // never been banned
	}
	return time.Now().UnixNano() < bannedUntil
}

// getBannedUntil returns the ban expiration time as a time.Time
func (w *worker) getBannedUntil() time.Time {
	nanos := atomic.LoadInt64(&w.bannedUntilNanos)
	if nanos == 0 {
		return time.Time{} // zero time
	}
	return time.Unix(0, nanos)
}

// setBannedUntil sets the ban expiration time atomically
func (w *worker) setBannedUntil(t time.Time) {
	var nanos int64
	if !t.IsZero() {
		nanos = t.UnixNano()
	}
	atomic.StoreInt64(&w.bannedUntilNanos, nanos)
}

// getConsecutiveFails returns the current consecutive failure count
func (w *worker) getConsecutiveFails() int {
	return int(atomic.LoadInt64(&w.consecutiveFails))
}

// workerManager manages workers and their ban status. It tracks workers, bans
// them based on rules, and resets their status after the ban period.
type workerManager struct {
	workers        sync.Map
	innerCtx       context.Context
	cancelFunc     context.CancelFunc
	rules          banRules
	tickerInterval time.Duration
}

// newWorkerManager creates a new worker manager with the specified ban rules.
// It initializes the worker map and sets up the context for managing workers.
// An optional ticker interval can be provided; defaults to 10 seconds if not specified.
func newWorkerManager(rules banRules, tickerInterval ...time.Duration) *workerManager {
	interval := 10 * time.Second // default production interval
	if len(tickerInterval) > 0 {
		interval = tickerInterval[0]
	}
	return &workerManager{
		workers:        sync.Map{},
		rules:          rules,
		tickerInterval: interval,
	}
}

// start initializes the worker manager, setting up a context for managing
// workers. It starts a goroutine that periodically checks for banned workers,
// bans them if necessary, and resets their status after the ban period.
func (wm *workerManager) start(ctx context.Context) {
	wm.innerCtx, wm.cancelFunc = context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(wm.tickerInterval)
		for {
			defer ticker.Stop()

			select {
			case <-ctx.Done():
				// Stop the worker manager when the context is done
				wm.stop()
				return
			case <-ticker.C:
				banned := wm.bannedWorkers()
				for _, w := range banned {
					bannedUntil := w.getBannedUntil()
					if bannedUntil.IsZero() {
						// Ban the worker for the configured timeout
						wm.setBanDuration(w.ID)
					} else if time.Now().After(bannedUntil) {
						// Unban the worker after the ban period
						wm.resetWorker(w.ID)
					}
				}
			}
		}
	}()
	log.Info("Worker manager started")
}

// stop stops the worker manager, cancels the context, and clears all workers.
// It ensures that all workers are removed and no further actions are taken.
// This is typically called when the application is shutting down or when
// the worker manager is no longer needed.
func (wm *workerManager) stop() {
	if wm.cancelFunc != nil {
		wm.cancelFunc()
	}
	// clear workers safely
	wm.workers.Range(func(key, value any) bool {
		wm.workers.Delete(key)
		return true // continue iteration
	})
}

// addWorker adds a new worker to the manager. If the worker already exists,
// it returns the existing worker without adding a new one. If it's a new
// worker, it initializes a new worker instance, stores it in the worker map,
// and returns the worker instance.
func (wm *workerManager) addWorker(id string) *worker {
	if w, exists := wm.getWorker(id); exists {
		log.Warnf("Worker %s already exists, not adding again", id)
		// Worker already exists, no need to add again
		return w
	}
	w := &worker{ID: id}
	wm.workers.Store(id, w)
	log.Debugf("Worker added: %s", id)
	return w
}

// getWorker retrieves a worker by its ID. If the worker exists, it returns
// the worker instance and a boolean indicating success. If the worker does
// not exist, it returns nil and false.
func (wm *workerManager) getWorker(id string) (*worker, bool) {
	if w, ok := wm.workers.Load(id); ok {
		return w.(*worker), true
	}
	return nil, false
}

// bannedWorkers returns a slice of workers that are currently banned based on
// the ban rules. It iterates through all workers in the manager, checks if
// they are banned according to the rules, and collects them in a slice.
func (wm *workerManager) bannedWorkers() []*worker {
	var banned []*worker
	wm.workers.Range(func(key, value any) bool {
		if w, ok := value.(*worker); ok && w.isBanned(wm.rules) {
			banned = append(banned, w)
		}
		return true // continue iteration
	})
	log.Debugf("Banned workers: %d", len(banned))
	return banned
}

// resetWorker resets the worker's status by creating a new worker instance
// with the same ID. This effectively clears the worker's consecutive fails
// and banned status, allowing the worker to be reused without any previous
// restrictions. It is typically called when a worker has been banned and
// the ban period has expired, or when a worker needs to be reset for any
// reason.
func (wm *workerManager) resetWorker(id string) {
	if _, ok := wm.getWorker(id); ok {
		wm.workers.Store(id, &worker{ID: id})
		log.Debugf("Worker %s reset", id)
	}
}

// setBanDuration sets the ban duration for a worker. It updates the worker's
// ban expiration time to the current time plus the ban timeout defined in the
// rules. This effectively bans the worker for the specified duration,
// preventing it from processing jobs until the ban period expires.
func (wm *workerManager) setBanDuration(workerID string) {
	if w, ok := wm.getWorker(workerID); ok {
		banTime := time.Now().Add(wm.rules.timeout)
		w.setBannedUntil(banTime)
		log.Warnf("Worker %s banned until %s", workerID, banTime)
	}
}

// workerResult updates the worker's status based on the result of a job.
// If the job was successful, it resets the worker's consecutive fails to zero.
// If the job failed, it increments the worker's consecutive fails count.
// This method uses atomic operations to ensure thread safety.
func (wm *workerManager) workerResult(id string, success bool) {
	w, ok := wm.getWorker(id)
	if !ok {
		w = &worker{ID: id}
		wm.workers.Store(id, w)
	}

	if success {
		atomic.StoreInt64(&w.consecutiveFails, 0)
	} else {
		atomic.AddInt64(&w.consecutiveFails, 1)
	}
}
