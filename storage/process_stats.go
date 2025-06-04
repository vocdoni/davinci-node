package storage

import (
	"fmt"

	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// ProcessStatsUpdate represents a single stats update operation
type ProcessStatsUpdate struct {
	TypeStats types.TypeStats
	Delta     int
}

// updateProcessStats updates multiple process stats in a single operation.
// This method assumes the caller already holds the globalLock.
func (s *Storage) updateProcessStats(pid []byte, updates []ProcessStatsUpdate) error {
	if pid == nil {
		return fmt.Errorf("nil process ID")
	}

	if len(updates) == 0 {
		return nil
	}

	p := &types.Process{}
	if err := s.getArtifact(processPrefix, pid, p); err != nil {
		return fmt.Errorf("failed to get process for stats update: %w", err)
	}

	for _, update := range updates {
		switch update.TypeStats {
		case types.TypeStatsStateTransitions:
			p.SequencerStats.StateTransitionCount += update.Delta
		case types.TypeStatsSettledStateTransitions:
			p.SequencerStats.SettledStateTransitionCount += update.Delta
		case types.TypeStatsAggregatedVotes:
			p.SequencerStats.AggregatedVotesCount += update.Delta
		case types.TypeStatsVerifiedVotes:
			p.SequencerStats.VerifiedVotesCount += update.Delta
		case types.TypeStatsPendingVotes:
			p.SequencerStats.PendingVotesCount += update.Delta
		case types.TypeStatsLastBatchSize:
			p.SequencerStats.LastBatchSize = update.Delta
		case types.TypeStatsCurrentBatchSize:
			p.SequencerStats.CurrentBatchSize += update.Delta
		default:
			return fmt.Errorf("unknown type stats: %d", update.TypeStats)
		}
	}

	if err := s.setArtifact(processPrefix, pid, p); err != nil {
		return fmt.Errorf("failed to save process after stats update: %w", err)
	}

	return nil
}

// TotalPendingBallots returns the total number of pending ballots across all processes
// by summing up the PendingVotesCount from each process's stats.
func (s *Storage) TotalPendingBallots() (int, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	pids, err := s.listArtifacts(processPrefix)
	if err != nil {
		return 0, fmt.Errorf("failed to list processes: %w", err)
	}

	totalPending := 0
	for _, pid := range pids {
		p := &types.Process{}
		if err := s.getArtifact(processPrefix, pid, p); err != nil {
			// Skip processes that can't be loaded
			continue
		}
		totalPending += p.SequencerStats.PendingVotesCount
	}

	return totalPending, nil
}
