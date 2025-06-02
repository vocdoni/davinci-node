package storage

import (
	"fmt"

	"go.vocdoni.io/dvote/db/prefixeddb"
)

var workerStatsPrefix = []byte("ws/")

// WorkerStats represents the statistics for a worker node
type WorkerStats struct {
	SuccessCount int64 `json:"successCount"`
	FailedCount  int64 `json:"failedCount"`
}

// IncreaseWorkerJobCount increases the success job count for a worker
func (s *Storage) IncreaseWorkerJobCount(address string, delta int64) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	stats := s.getWorkerStatsUnsafe(address)
	stats.SuccessCount += delta

	return s.setWorkerStatsUnsafe(address, stats)
}

// IncreaseWorkerFailedJobCount increases the failed job count for a worker
func (s *Storage) IncreaseWorkerFailedJobCount(address string, delta int64) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	stats := s.getWorkerStatsUnsafe(address)
	stats.FailedCount += delta

	return s.setWorkerStatsUnsafe(address, stats)
}

// WorkerJobCount returns the success and failed job counts for a worker
func (s *Storage) WorkerJobCount(address string) (success, failed int64, err error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	stats := s.getWorkerStatsUnsafe(address)
	return stats.SuccessCount, stats.FailedCount, nil
}

// ListWorkerJobCount returns a map of all workers and their job counts
// The returned map has the format: address -> [successCount, failedCount]
func (s *Storage) ListWorkerJobCount() (map[string][2]int64, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	result := make(map[string][2]int64)

	pr := prefixeddb.NewPrefixedReader(s.db, workerStatsPrefix)
	err := pr.Iterate(nil, func(k, v []byte) bool {
		var stats WorkerStats
		if err := DecodeArtifact(v, &stats); err != nil {
			// Skip invalid records
			return true
		}

		address := string(k)
		result[address] = [2]int64{stats.SuccessCount, stats.FailedCount}
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("failed to iterate worker stats: %w", err)
	}

	return result, nil
}

// getWorkerStatsUnsafe retrieves worker stats without locking (internal use only)
func (s *Storage) getWorkerStatsUnsafe(address string) *WorkerStats {
	var stats WorkerStats
	key := []byte(address)

	if err := s.getArtifact(workerStatsPrefix, key, &stats); err != nil {
		// Return empty stats if not found
		return &WorkerStats{
			SuccessCount: 0,
			FailedCount:  0,
		}
	}

	return &stats
}

// setWorkerStatsUnsafe stores worker stats without locking (internal use only)
func (s *Storage) setWorkerStatsUnsafe(address string, stats *WorkerStats) error {
	key := []byte(address)
	return s.setArtifact(workerStatsPrefix, key, stats)
}
