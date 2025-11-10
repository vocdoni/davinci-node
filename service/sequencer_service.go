package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/sequencer"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

const (
	// SequencerStatsEndpoint is the API endpoint for retrieving sequencer statistics.
	SequencerStatsEndpoint = "/sequencer/stats"
)

// StatsMonitorInterval is the interval at which process statistics are logged.
// This can be overridden before starting the service.
var StatsMonitorInterval = 60 * time.Second

// SequencerService represents a service that handles background vote processing.
type SequencerService struct {
	Sequencer          *sequencer.Sequencer
	storage            *storage.Storage
	api                *api.API
	activeProcessCount atomic.Int32 // Count of active processes accepting votes
}

// NewSequencer creates a new sequencer instance. It will verify new votes, aggregate them into batches,
// and update the ongoing state with the new ones. The batchTimeWindow defines how long a batch can wait
// until processed (either the batch becomes full of votes or the time window expires).
func NewSequencer(stg *storage.Storage, contracts *web3.Contracts, batchTimeWindow time.Duration, api *api.API) *SequencerService {
	s, err := sequencer.New(stg, contracts, batchTimeWindow)
	if err != nil {
		log.Fatalf("failed to create sequencer: %v", err)
	}
	return &SequencerService{
		Sequencer: s,
		storage:   stg,
		api:       api,
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

	// Register the stats handler for the API
	if ss.api != nil {
		ss.api.Router().Get(SequencerStatsEndpoint, ss.statsHandler)
		log.Infow("register handler", "endpoint", SequencerStatsEndpoint, "method", "GET")
	}

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
		isAcceptingVotes, err := ss.storage.ProcessIsAcceptingVotes(processID)
		if err != nil || !isAcceptingVotes {
			continue
		}

		activeProcessCount++
		stats := process.SequencerStats

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
			"lastTransitionTime": stats.LastStateTransitionDate.Format(time.RFC3339),
		})
	}

	totalStats, err := ss.storage.TotalStats()
	if err != nil {
		log.Errorw(err, "failed to get total stats")
		return
	}
	pendingVotes := ss.storage.TotalPendingBallots()

	// Log summary statistics
	log.Monitor("active process statistics summary", map[string]any{
		"pendingVotes":       pendingVotes,
		"verifiedVotes":      totalStats.VerifiedVotesCount,
		"aggregatedVotes":    totalStats.AggregatedVotesCount,
		"stateTransitions":   totalStats.StateTransitionCount,
		"settledTransitions": totalStats.SettledStateTransitionCount,
		"lastTransitionTime": totalStats.LastStateTransitionDate.Format(time.RFC3339),
		"activeProcesses":    activeProcessCount,
	})
	// Update the sequencer stats
	ss.activeProcessCount.Store(int32(activeProcessCount))
}

// statsHandler is an HTTP handler that returns the current statistics of the sequencer service.
func (ss *SequencerService) statsHandler(w http.ResponseWriter, r *http.Request) {
	sstats, err := ss.storage.TotalStats()
	if err != nil {
		api.ErrResourceNotFound.WithErr(err).Write(w)
		return
	}
	stats := &api.SequencerStatsResponse{
		Stats:           *sstats,
		ActiveProcesses: int(ss.activeProcessCount.Load()),
		PendingVotes:    ss.storage.TotalPendingBallots(),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	jdata, err := json.Marshal(stats)
	if err != nil {
		api.ErrMarshalingServerJSONFailed.WithErr(err).Write(w)
		return
	}
	if _, err = w.Write(jdata); err != nil {
		log.Warnw("failed to write http response", "error", err)
		return
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		log.Warnw("failed to write on response", "error", err)
		return
	}
}
