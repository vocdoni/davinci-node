package workers

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
)

type WorkerInfo struct {
	Address      string `json:"address"`
	Name         string `json:"name"`
	SuccessCount int64  `json:"successCount"`
	FailedCount  int64  `json:"failedCount"`
}

// WorkerBanRules defines the rules for banning workers. It includes the
// duration for which a worker is banned and the maximum number of consecutive
// failed jobs before a worker is banned.
type WorkerBanRules struct {
	BanTimeout          time.Duration // Duration for which the worker is banned
	FailuresToGetBanned int           // Maximum consecutive failed jobs before banning
}

// DefaultWorkerBanRules provides the default ban rules for workers
var DefaultWorkerBanRules = &WorkerBanRules{
	BanTimeout:          30 * time.Minute, // Ban for 30 minutes
	FailuresToGetBanned: 3,                // 3 consecutive failed jobs
}

// Worker represents a Worker that processes jobs
type Worker struct {
	Address          string
	Name             string // Name of the worker for identification
	consecutiveFails int64  // atomic counter
	bannedUntilNanos int64  // atomic Unix nanoseconds, 0 = not banned
}

// IsBanned checks if the worker is banned based on the provided rules
func (w *Worker) IsBanned(rules *WorkerBanRules) bool {
	if rules == nil {
		return false // no rules provided, not banned
	}
	consecutiveFails := atomic.LoadInt64(&w.consecutiveFails)
	if consecutiveFails > int64(rules.FailuresToGetBanned) {
		return true
	}

	// Check time-based ban
	bannedUntil := atomic.LoadInt64(&w.bannedUntilNanos)
	if bannedUntil == 0 {
		return false // never been banned
	}
	return time.Now().UnixNano() < bannedUntil
}

// GetBannedUntil returns the ban expiration time as a time.Time
func (w *Worker) GetBannedUntil() time.Time {
	nanos := atomic.LoadInt64(&w.bannedUntilNanos)
	if nanos == 0 {
		return time.Time{} // zero time
	}
	return time.Unix(0, nanos)
}

// SetBannedUntil sets the ban expiration time atomically
func (w *Worker) SetBannedUntil(t time.Time) {
	var nanos int64
	if !t.IsZero() {
		nanos = t.UnixNano()
	}
	atomic.StoreInt64(&w.bannedUntilNanos, nanos)
}

// SetConsecutiveFails returns the current consecutive failure count
func (w *Worker) SetConsecutiveFails() int {
	return int(atomic.LoadInt64(&w.consecutiveFails))
}

// WorkerManager manages workers and their ban status. It tracks workers, bans
// them based on rules, and resets their status after the ban period.
type WorkerManager struct {
	stg            *storage.Storage
	workers        sync.Map
	innerCtx       context.Context
	cancelFunc     context.CancelFunc
	rules          *WorkerBanRules
	tickerInterval time.Duration
}

// NewWorkerManager creates a new worker manager with the specified ban rules.
// It initializes the worker map and sets up the context for managing workers.
// An optional ticker interval can be provided; defaults to 10 seconds if not specified.
func NewWorkerManager(stg *storage.Storage, rules *WorkerBanRules, tickerInterval ...time.Duration) *WorkerManager {
	interval := 10 * time.Second // default production interval
	if len(tickerInterval) > 0 {
		interval = tickerInterval[0]
	}
	banRules := DefaultWorkerBanRules
	if rules != nil {
		banRules = rules
	}
	return &WorkerManager{
		stg:            stg,
		workers:        sync.Map{},
		rules:          banRules,
		tickerInterval: interval,
	}
}

// Start initializes the worker manager, setting up a context for managing
// workers. It starts a goroutine that periodically checks for banned workers,
// bans them if necessary, and resets their status after the ban period.
func (wm *WorkerManager) Start(ctx context.Context) {
	wm.innerCtx, wm.cancelFunc = context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(wm.tickerInterval)
		for {
			defer ticker.Stop()

			select {
			case <-ctx.Done():
				// Stop the worker manager when the context is done
				wm.Stop()
				return
			case <-ticker.C:
				banned := wm.BannedWorkers()
				for _, w := range banned {
					bannedUntil := w.GetBannedUntil()
					if bannedUntil.IsZero() {
						// Ban the worker for the configured timeout
						wm.SetBanDuration(w.Address)
					} else if time.Now().After(bannedUntil) {
						// Unban the worker after the ban period
						wm.ResetWorker(w.Address)
					}
				}
			}
		}
	}()
	log.Infow("worker manager started",
		"banTimeout", wm.rules.BanTimeout.String(),
		"failuresToGetBanned", wm.rules.FailuresToGetBanned,
		"tickerInterval", wm.tickerInterval.String())
}

// Stop stops the worker manager, cancels the context, and clears all workers.
// It ensures that all workers are removed and no further actions are taken.
// This is typically called when the application is shutting down or when
// the worker manager is no longer needed.
func (wm *WorkerManager) Stop() {
	if wm.cancelFunc != nil {
		wm.cancelFunc()
	}
	// clear workers safely
	wm.workers.Range(func(key, value any) bool {
		wm.workers.Delete(key)
		return true // continue iteration
	})
}

// AddWorker adds a new worker to the manager. If the worker already exists,
// it returns the existing worker without adding a new one. If it's a new
// worker, it initializes a new worker instance, stores it in the worker map,
// and returns the worker instance.
func (wm *WorkerManager) AddWorker(address, name string) *Worker {
	// Worker already exists, no need to add again
	if w, exists := wm.GetWorker(address); exists {
		// Update the worker's name if provided and different from the existing
		// one
		if name != "" && w.Name == "" {
			w.Name = name
			wm.workers.Store(address, w)
		}
		return w
	}
	w := &Worker{
		Address: address,
		Name:    name, // Set the worker's name for identification
	}
	wm.workers.Store(address, w)
	log.Debugw("worker added", "address", address, "name", name)
	return w
}

// GetWorker retrieves a worker by its address. If the worker exists, it
// returns the worker instance and a boolean indicating success. If the
// worker does not exist, it returns nil and false.
func (wm *WorkerManager) GetWorker(address string) (*Worker, bool) {
	if w, ok := wm.workers.Load(address); ok {
		return w.(*Worker), true
	}
	return nil, false
}

// BannedWorkers returns a slice of workers that are currently banned based on
// the ban rules. It iterates through all workers in the manager, checks if
// they are banned according to the rules, and collects them in a slice.
func (wm *WorkerManager) BannedWorkers() []*Worker {
	var banned []*Worker
	wm.workers.Range(func(key, value any) bool {
		if w, ok := value.(*Worker); ok && w.IsBanned(wm.rules) {
			banned = append(banned, w)
		}
		return true // continue iteration
	})
	return banned
}

// ResetWorker resets the worker's status by creating a new worker instance
// with the same address. This effectively clears the worker's consecutive
// fails and banned status, allowing the worker to be reused without any
// previous restrictions. It is typically called when a worker has been
// banned and the ban period has expired, or when a worker needs to be
// reset for any reason.
func (wm *WorkerManager) ResetWorker(address string) {
	if _, ok := wm.GetWorker(address); ok {
		wm.workers.Store(address, &Worker{Address: address})
		log.Debugw("worker reset", "address", address)
	}
}

// SetBanDuration sets the ban duration for a worker. It updates the worker's
// ban expiration time to the current time plus the ban timeout defined in the
// rules. This effectively bans the worker for the specified duration,
// preventing it from processing jobs until the ban period expires.
func (wm *WorkerManager) SetBanDuration(address string) {
	if w, ok := wm.GetWorker(address); ok {
		banTime := time.Now().Add(wm.rules.BanTimeout)
		w.SetBannedUntil(banTime)
		log.Warnw("worker banned", "address", address, "until", banTime.String())
	}
}

// WorkerResult updates the worker's status based on the result of a job.
// If the job was successful, it resets the worker's consecutive fails to zero.
// If the job failed, it increments the worker's consecutive fails count.
// This method uses atomic operations to ensure thread safety.
func (wm *WorkerManager) WorkerResult(address string, success bool) error {
	w, ok := wm.GetWorker(address)
	if !ok {
		w = &Worker{Address: address}
		wm.workers.Store(address, w)
	}

	if success {
		atomic.StoreInt64(&w.consecutiveFails, 0)
		return wm.stg.IncreaseWorkerJobCount(address, 1)
	} else {
		atomic.AddInt64(&w.consecutiveFails, 1)
		return wm.stg.IncreaseWorkerFailedJobCount(address, 1)
	}
}

// WorkerStats retrieves the statistics for a specific worker by its ID. The
// statistics include the worker's name, success count, and failed count. If
// the worker does not exist, it returns an error indicating that the worker
// was not found.
func (wm *WorkerManager) WorkerStats(address string) (*WorkerInfo, error) {
	w, ok := wm.GetWorker(address)
	if !ok {
		return nil, fmt.Errorf("worker %s not found", address)
	}
	name := w.Name
	if name == "" {
		if hiddenAddr, err := WorkerNameFromAddress(address); err == nil {
			name = hiddenAddr
		}
	}
	stats := wm.stg.WorkerStats(address)
	return &WorkerInfo{
		Address:      address,
		Name:         name,
		SuccessCount: stats.SuccessCount,
		FailedCount:  stats.FailedCount,
	}, nil
}

// ListWorkerStats retrieves the statistics for all workers managed by the
// WorkerManager. It returns a slice of WorkerInfo containing the address,
// name, success count, and failed count for each worker. If there is an error
// retrieving the statistics, it returns an error.
func (wm *WorkerManager) ListWorkerStats() ([]*WorkerInfo, error) {
	workerStats, err := wm.stg.ListWorkerJobCount()
	if err != nil {
		return nil, fmt.Errorf("failed to list worker stats: %w", err)
	}

	result := []*WorkerInfo{}
	wm.workers.Range(func(key, value any) bool {
		worker, ok := value.(*Worker)
		if !ok {
			return true // continue iteration
		}
		// By default use the worker's name, if not set, obfuscate the address
		name := worker.Name
		if name == "" {
			if hiddenAddr, err := WorkerNameFromAddress(worker.Address); err == nil {
				name = hiddenAddr
			}
		}
		// By default, success and failed counts are 0, if the worker exists in
		// the stats map, use those values
		var success, failed int64
		if stats, ok := workerStats[worker.Address]; ok {
			success = stats.SuccessCount
			failed = stats.FailedCount
		}
		result = append(result, &WorkerInfo{
			Address:      worker.Address,
			Name:         name,
			SuccessCount: success,
			FailedCount:  failed,
		})
		return true
	})
	return result, nil
}
