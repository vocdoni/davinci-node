package service

import (
	"context"
	"time"

	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/sequencer"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/web3"
)

// SequencerService represents a service that handles background vote processing.
type SequencerService struct {
	Sequencer *sequencer.Sequencer
}

// NewSequencer creates a new sequencer instance. It will verify new votes, aggregate them into batches,
// and update the ongoing state with the new ones. The batchTimeWindow defines how long a batch can wait
// until processed (either the batch becomes full of votes or the time window expires).
func NewSequencer(stg *storage.Storage, contracts *web3.Contracts, batchTimeWindow time.Duration) *SequencerService {
	log.Infow("creating sequencer service", "batchTimeWindow", batchTimeWindow.String())
	s, err := sequencer.New(stg, contracts, batchTimeWindow)
	if err != nil {
		log.Fatalf("failed to create sequencer: %v", err)
	}
	return &SequencerService{
		Sequencer: s,
	}
}

// Start begins the vote processing service. It returns an error if the service is already running.
func (ss *SequencerService) Start(ctx context.Context) error {
	return ss.Sequencer.Start(ctx)
}

// Stop halts the vote processing service.
func (ss *SequencerService) Stop() {
	if err := ss.Sequencer.Stop(); err != nil {
		log.Warnw("sequencer service stopped", "error", err)
	}
}
