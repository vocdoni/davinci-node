package service

import (
	"context"
	"fmt"
	"time"

	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/sequencer"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"github.com/vocdoni/vocdoni-z-sandbox/web3"
)

// StatsMonitorInterval is the interval at which process statistics are logged.
// This can be overridden before starting the service.
var StatsMonitorInterval = 60 * time.Second

// SequencerService represents a service that handles background vote processing.
type SequencerService struct {
	Sequencer *sequencer.Sequencer
	storage   *storage.Storage
}

// NewSequencer creates a new sequencer instance. It will verify new votes, aggregate them into batches,
// and update the ongoing state with the new ones. The batchTimeWindow defines how long a batch can wait
// until processed (either the batch becomes full of votes or the time window expires).
func NewSequencer(stg *storage.Storage, contracts *web3.Contracts, batchTimeWindow time.Duration) *SequencerService {
	s, err := sequencer.New(stg, contracts, batchTimeWindow)
	if err != nil {
		log.Fatalf("failed to create sequencer: %v", err)
	}
	return &SequencerService{
		Sequencer: s,
		storage:   stg,
	}
}

// Start begins the vote processing service. It returns an error if the service is already running.
func (ss *SequencerService) Start(ctx context.Context) error {
	// Start the sequencer
	if err := ss.Sequencer.Start(ctx); err != nil {
		return err
	}

	// Start the stats monitor
	ss.startStatsMonitor(ctx, StatsMonitorInterval)

	return nil
}

// Stop halts the vote processing service.
func (ss *SequencerService) Stop() {
	if err := ss.Sequencer.Stop(); err != nil {
		log.Warnw("sequencer service stopped", "error", err)
	}
}

// startStatsMonitor starts a goroutine that periodically logs statistics
// for all active processes (those accepting votes).
func (ss *SequencerService) startStatsMonitor(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		log.Infow("process stats monitor started", "interval", interval.String())

		for {
			select {
			case <-ctx.Done():
				log.Infow("process stats monitor stopped")
				return
			case <-ticker.C:
				ss.logActiveProcessStats()
			}
		}
	}()
}

// logActiveProcessStats logs statistics for all processes that are actively
// accepting votes. It uses the sequencer's internal process ID map to efficiently
// track which processes are active.
func (ss *SequencerService) logActiveProcessStats() {
	// Track total stats across all processes
	var totalPending, totalVerified, totalAggregated, totalStateTransitions, totalSettledStateTransitions int
	activeProcessCount := 0

	// Iterate through active processes using the sequencer's process ID map
	for _, pid := range ss.Sequencer.ActiveProcessIDs() {
		processID := new(types.ProcessID).SetBytes(pid)
		process, err := ss.storage.Process(processID)
		if err != nil {
			log.Warnw("failed to get process for stats", "processID", fmt.Sprintf("%x", pid), "error", err)
			continue
		}

		// Only log if the process is accepting votes
		if !process.IsAcceptingVotes || process.IsFinalized {
			continue
		}

		activeProcessCount++
		stats := process.SequencerStats

		// Accumulate totals
		totalPending += stats.PendingVotesCount
		totalVerified += stats.VerifiedVotesCount
		totalAggregated += stats.AggregatedVotesCount
		totalStateTransitions += stats.StateTransitionCount
		totalSettledStateTransitions += stats.SettledStateTransitionCount

		// Skip processes with no verified votes
		if stats.VerifiedVotesCount == 0 {
			continue
		}

		// Log individual process stats
		log.Monitor(fmt.Sprintf("process %s", processID.String()), map[string]any{
			"pendingVotes":       stats.PendingVotesCount,
			"verifiedVotes":      stats.VerifiedVotesCount,
			"aggregatedVotes":    stats.AggregatedVotesCount,
			"currentBatchSize":   stats.CurrentBatchSize,
			"lastBatchSize":      stats.LastBatchSize,
			"stateTransitions":   stats.StateTransitionCount,
			"settledTransitions": stats.SettledStateTransitionCount,
			"lastTransitionTime": stats.LasStateTransitionDate.Format(time.RFC3339),
		})
	}

	// Log summary statistics
	log.Monitor("global statistics summary", map[string]any{
		"active":             activeProcessCount,
		"pendingVotes":       totalPending,
		"verifiedVotes":      totalVerified,
		"aggregatedVotes":    totalAggregated,
		"stateTransitions":   totalStateTransitions,
		"settledTransitions": totalSettledStateTransitions,
		"activeProcesses":    activeProcessCount,
	})
}
