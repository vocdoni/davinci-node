package storage

import (
	"fmt"

	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
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

	// DEBUG: Log the current state before any updates
	log.Infow("DEBUG: updateProcessStats BEFORE",
		"processID", fmt.Sprintf("%x", pid),
		"verifiedVotes", p.SequencerStats.VerifiedVotesCount,
		"pendingVotes", p.SequencerStats.PendingVotesCount,
		"currentBatchSize", p.SequencerStats.CurrentBatchSize,
		"aggregatedVotes", p.SequencerStats.AggregatedVotesCount,
		"updates", fmt.Sprintf("%+v", updates),
	)

	for _, update := range updates {
		switch update.TypeStats {
		case types.TypeStatsStateTransitions:
			oldValue := p.SequencerStats.StateTransitionCount
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
			log.Infow("DEBUG: StateTransitionCount update",
				"processID", fmt.Sprintf("%x", pid),
				"oldValue", oldValue,
				"delta", update.Delta,
				"newValue", p.SequencerStats.StateTransitionCount,
			)
		case types.TypeStatsSettledStateTransitions:
			oldValue := p.SequencerStats.SettledStateTransitionCount
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
			log.Infow("DEBUG: SettledStateTransitionCount update",
				"processID", fmt.Sprintf("%x", pid),
				"oldValue", oldValue,
				"delta", update.Delta,
				"newValue", p.SequencerStats.SettledStateTransitionCount,
			)
		case types.TypeStatsAggregatedVotes:
			oldValue := p.SequencerStats.AggregatedVotesCount
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
			log.Infow("DEBUG: AggregatedVotesCount update",
				"processID", fmt.Sprintf("%x", pid),
				"oldValue", oldValue,
				"delta", update.Delta,
				"newValue", p.SequencerStats.AggregatedVotesCount,
			)
		case types.TypeStatsVerifiedVotes:
			oldValue := p.SequencerStats.VerifiedVotesCount
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
			log.Infow("DEBUG: VerifiedVotesCount update",
				"processID", fmt.Sprintf("%x", pid),
				"oldValue", oldValue,
				"delta", update.Delta,
				"newValue", p.SequencerStats.VerifiedVotesCount,
			)
		case types.TypeStatsPendingVotes:
			oldValue := p.SequencerStats.PendingVotesCount
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
			log.Infow("DEBUG: PendingVotesCount update",
				"processID", fmt.Sprintf("%x", pid),
				"oldValue", oldValue,
				"delta", update.Delta,
				"newValue", p.SequencerStats.PendingVotesCount,
			)
		case types.TypeStatsLastBatchSize:
			oldValue := p.SequencerStats.LastBatchSize
			if update.Delta < 0 {
				log.Warnw("attempted to set negative LastBatchSize, clamping to 0",
					"processID", fmt.Sprintf("%x", pid),
					"delta", update.Delta,
				)
				p.SequencerStats.LastBatchSize = 0
			} else {
				p.SequencerStats.LastBatchSize = update.Delta
			}
			log.Infow("DEBUG: LastBatchSize update",
				"processID", fmt.Sprintf("%x", pid),
				"oldValue", oldValue,
				"delta", update.Delta,
				"newValue", p.SequencerStats.LastBatchSize,
			)
		case types.TypeStatsCurrentBatchSize:
			oldValue := p.SequencerStats.CurrentBatchSize
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
			log.Infow("DEBUG: CurrentBatchSize update",
				"processID", fmt.Sprintf("%x", pid),
				"oldValue", oldValue,
				"delta", update.Delta,
				"newValue", p.SequencerStats.CurrentBatchSize,
			)
		default:
			return fmt.Errorf("unknown type stats: %d", update.TypeStats)
		}
	}

	// DEBUG: Log the final state after all updates
	log.Infow("DEBUG: updateProcessStats AFTER",
		"processID", fmt.Sprintf("%x", pid),
		"verifiedVotes", p.SequencerStats.VerifiedVotesCount,
		"pendingVotes", p.SequencerStats.PendingVotesCount,
		"currentBatchSize", p.SequencerStats.CurrentBatchSize,
		"aggregatedVotes", p.SequencerStats.AggregatedVotesCount,
	)

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
