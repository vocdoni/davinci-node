package storage

import (
	"fmt"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// ProcessStatsUpdate represents a single stats update operation
type ProcessStatsUpdate struct {
	TypeStats types.TypeStats
	Delta     int
}

// updateProcessStats updates multiple process stats in a single operation.
// This method assumes the caller already holds the globalLock.
// NOTE: This method should NOT be converted to use UpdateProcess as it would cause nested locking.
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
			newValue := p.SequencerStats.StateTransitionCount + update.Delta
			if newValue < 0 {
				log.Warnw("attempted to set negative StateTransitionCount, clamping to 0",
					"processID", fmt.Sprintf("%x", pid),
					"currentValue", p.SequencerStats.StateTransitionCount,
					"delta", update.Delta,
				)
				p.SequencerStats.StateTransitionCount = 0
			} else {
				p.SequencerStats.StateTransitionCount = newValue
			}
		case types.TypeStatsSettledStateTransitions:
			newValue := p.SequencerStats.SettledStateTransitionCount + update.Delta
			if newValue < 0 {
				log.Warnw("attempted to set negative SettledStateTransitionCount, clamping to 0",
					"processID", fmt.Sprintf("%x", pid),
					"currentValue", p.SequencerStats.SettledStateTransitionCount,
					"delta", update.Delta,
				)
				p.SequencerStats.SettledStateTransitionCount = 0
			} else {
				p.SequencerStats.SettledStateTransitionCount = newValue
			}
		case types.TypeStatsAggregatedVotes:
			newValue := p.SequencerStats.AggregatedVotesCount + update.Delta
			if newValue < 0 {
				log.Warnw("attempted to set negative AggregatedVotesCount, clamping to 0",
					"processID", fmt.Sprintf("%x", pid),
					"currentValue", p.SequencerStats.AggregatedVotesCount,
					"delta", update.Delta,
				)
				p.SequencerStats.AggregatedVotesCount = 0
			} else {
				p.SequencerStats.AggregatedVotesCount = newValue
			}
		case types.TypeStatsVerifiedVotes:
			newValue := p.SequencerStats.VerifiedVotesCount + update.Delta
			if newValue < 0 {
				log.Warnw("attempted to set negative VerifiedVotesCount, clamping to 0",
					"processID", fmt.Sprintf("%x", pid),
					"currentValue", p.SequencerStats.VerifiedVotesCount,
					"delta", update.Delta,
				)
				p.SequencerStats.VerifiedVotesCount = 0
			} else {
				p.SequencerStats.VerifiedVotesCount = newValue
			}
		case types.TypeStatsPendingVotes:
			newValue := p.SequencerStats.PendingVotesCount + update.Delta
			if newValue < 0 {
				log.Warnw("attempted to set negative PendingVotesCount, clamping to 0",
					"processID", fmt.Sprintf("%x", pid),
					"currentValue", p.SequencerStats.PendingVotesCount,
					"delta", update.Delta,
				)
				p.SequencerStats.PendingVotesCount = 0
			} else {
				p.SequencerStats.PendingVotesCount = newValue
			}
		case types.TypeStatsLastBatchSize:
			if update.Delta < 0 {
				log.Warnw("attempted to set negative LastBatchSize, clamping to 0",
					"processID", fmt.Sprintf("%x", pid),
					"delta", update.Delta,
				)
				p.SequencerStats.LastBatchSize = 0
			} else {
				p.SequencerStats.LastBatchSize = update.Delta
			}
		case types.TypeStatsCurrentBatchSize:
			newValue := p.SequencerStats.CurrentBatchSize + update.Delta
			if newValue < 0 {
				log.Warnw("attempted to set negative CurrentBatchSize, clamping to 0",
					"processID", fmt.Sprintf("%x", pid),
					"currentValue", p.SequencerStats.CurrentBatchSize,
					"delta", update.Delta,
				)
				p.SequencerStats.CurrentBatchSize = 0
			} else {
				p.SequencerStats.CurrentBatchSize = newValue
			}
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
