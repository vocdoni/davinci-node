package storage

import (
	"fmt"
	"time"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// totalStatsStorageKey is the key used to store total statistics across all processes.
var totalStatsStorageKey = []byte("totalStatsStorageKey")

// totalPendingBallotsKey is the key used to store the total number of pending ballots across all processes.
var totalPendingBallotsKey = []byte("totalPendingBallotsKey")

// updateProcessStats updates multiple process stats in a single operation.
// This method assumes the caller already holds the globalLock.
// NOTE: This method should NOT be converted to use UpdateProcess as it would cause nested locking.
func (s *Storage) updateProcessStats(processID types.ProcessID, updates []ProcessStatsUpdate) error {
	if !processID.IsValid() {
		return fmt.Errorf("invalid process ID")
	}

	if len(updates) == 0 {
		return nil
	}

	p := &types.Process{}
	if err := s.getArtifact(processPrefix, processID.Bytes(), p); err != nil {
		return fmt.Errorf("failed to get process for stats update: %w", err)
	}

	totalStats := &Stats{}
	if err := s.getArtifact(statsPrefix, totalStatsStorageKey, totalStats); err != nil {
		log.Debugw("initializing to zero sequencer stats")
		totalStats = new(Stats)
	}

	totalPending := &StatsPendingBallots{}
	if err := s.getArtifact(statsPrefix, totalPendingBallotsKey, totalPending); err != nil {
		log.Debugw("initializing to zero pending ballots stats")
		totalPending = new(StatsPendingBallots)
	}

	for _, update := range updates {
		switch update.TypeStats {
		case types.TypeStatsStateTransitions:
			p.SequencerStats.StateTransitionCount += update.Delta
			totalStats.StateTransitionCount += update.Delta
		case types.TypeStatsSettledStateTransitions:
			p.SequencerStats.SettledStateTransitionCount += update.Delta
			totalStats.SettledStateTransitionCount += update.Delta
		case types.TypeStatsAggregatedVotes:
			p.SequencerStats.AggregatedVotesCount += update.Delta
			totalStats.AggregatedVotesCount += update.Delta
		case types.TypeStatsVerifiedVotes:
			p.SequencerStats.VerifiedVotesCount += update.Delta
			totalStats.VerifiedVotesCount += update.Delta
		case types.TypeStatsPendingVotes:
			newValue := p.SequencerStats.PendingVotesCount + update.Delta
			if newValue < 0 {
				log.Warnw("attempted to set negative PendingVotesCount, clamping to 0",
					"processID", processID.String(),
					"currentValue", p.SequencerStats.PendingVotesCount,
					"delta", update.Delta,
				)
				totalPending.TotalPendingBallots -= p.SequencerStats.PendingVotesCount
				p.SequencerStats.PendingVotesCount = 0
			} else {
				p.SequencerStats.PendingVotesCount = newValue
				totalPending.TotalPendingBallots += update.Delta
			}
			totalPending.LastUpdateDate = time.Now()
		case types.TypeStatsLastBatchSize:
			if update.Delta < 0 {
				log.Warnw("attempted to set negative LastBatchSize, clamping to 0",
					"processID", processID.String(),
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
					"processID", processID.String(),
					"currentValue", p.SequencerStats.CurrentBatchSize,
					"delta", update.Delta,
				)
				p.SequencerStats.CurrentBatchSize = 0
			} else {
				p.SequencerStats.CurrentBatchSize = newValue
			}
		case types.TypeStatsLastTransitionDate:
			t := time.Now()
			p.SequencerStats.LastStateTransitionDate = t
			totalStats.LastStateTransitionDate = t
		default:
			return fmt.Errorf("unknown type stats: %d", update.TypeStats)
		}
	}

	if err := s.setArtifact(processPrefix, processID.Bytes(), p); err != nil {
		return fmt.Errorf("failed to save process after stats update: %w", err)
	}
	if err := s.setArtifact(statsPrefix, totalStatsStorageKey, totalStats); err != nil {
		return fmt.Errorf("failed to save total stats after process stats update: %w", err)
	}
	if err := s.setArtifact(statsPrefix, totalPendingBallotsKey, totalPending); err != nil {
		return fmt.Errorf("failed to save total pending ballots after process stats update: %w", err)
	}

	return nil
}

// TotalPendingBallots returns the total number of pending ballots across all processes
func (s *Storage) TotalPendingBallots() int {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	totalPending := &StatsPendingBallots{}
	if err := s.getArtifact(statsPrefix, totalPendingBallotsKey, totalPending); err != nil {
		return 0
	}
	return totalPending.TotalPendingBallots
}

// TotalStats returns the total statistics across all processes.
func (s *Storage) TotalStats() (*Stats, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	totalStats := &Stats{}
	if err := s.getArtifact(statsPrefix, totalStatsStorageKey, totalStats); err != nil {
		// If not found, return empty stats instead of error
		return &Stats{}, nil
	}
	return totalStats, nil
}
