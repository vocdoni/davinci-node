// Package sequencer provides functionality for processing and aggregating ballots
// into batches with zero-knowledge proofs for efficient verification.
package sequencer

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/aggregator"
	ballottest "github.com/vocdoni/vocdoni-z-sandbox/circuits/test/ballotproof"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/voteverifier"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
)

// Sequencer is a worker that takes verified ballots and aggregates them into a single proof.
// It processes ballots and creates batches of proofs for efficient verification.
type Sequencer struct {
	stg      *storage.Storage
	ctx      context.Context
	cancel   context.CancelFunc
	pids     map[string]time.Time // Maps process IDs to their last update time
	pidsLock sync.RWMutex         // Protects access to the pids map

	ballotVerifyingKeyCircomJSON []byte // Verification key for ballot proofs

	aggregateProvingKey groth16.ProvingKey          // Key for generating aggregate proofs
	aggregateCcs        constraint.ConstraintSystem // Constraint system for aggregate proofs

	voteProvingKey groth16.ProvingKey          // Key for generating vote proofs
	voteCcs        constraint.ConstraintSystem // Constraint system for vote proofs

	// maxTimeWindow is the maximum time window to wait for a batch to be processed.
	// If this time elapses, the batch will be processed even if not full.
	maxTimeWindow time.Duration
}

// New creates a new Sequencer instance that processes ballots and aggregates them into batches.
// It loads all necessary cryptographic artifacts for proof verification and generation.
//
// Parameters:
//   - stg: Storage instance for accessing ballots and other data
//   - batchTimeWindow: Maximum time to wait before processing a batch even if not full
//
// Returns a configured Sequencer instance or an error if initialization fails.
func New(stg *storage.Storage, batchTimeWindow time.Duration) (*Sequencer, error) {
	if stg == nil {
		return nil, fmt.Errorf("storage cannot be nil")
	}
	if batchTimeWindow <= 0 {
		return nil, fmt.Errorf("batch time window must be positive")
	}
	// Store the start time
	startTime := time.Now()

	// Load vote verifier artifacts
	vvArtifacts := voteverifier.Artifacts
	if err := vvArtifacts.LoadAll(); err != nil {
		return nil, fmt.Errorf("failed to load vote verifier artifacts: %w", err)
	}

	// Decode the vote verifier circuit definition
	log.Debugw("reading cicuit artifact",
		"circuit", "voteVerifier",
		"type", "ccs",
		"size", len(vvArtifacts.CircuitDefinition()),
	)
	voteCcs := groth16.NewCS(ecc.BLS12_377)
	if _, err := voteCcs.ReadFrom(bytes.NewReader(vvArtifacts.CircuitDefinition())); err != nil {
		return nil, fmt.Errorf("failed to read vote verifier definition: %w", err)
	}

	// Decode the vote verifier proving key
	log.Debugw("reading cicuit artifact",
		"circuit", "voteVerifier",
		"type", "pk",
		"size", len(vvArtifacts.ProvingKey()),
	)
	votePk := groth16.NewProvingKey(ecc.BLS12_377)
	if _, err := votePk.ReadFrom(bytes.NewReader(vvArtifacts.ProvingKey())); err != nil {
		return nil, fmt.Errorf("failed to read vote verifier proving key: %w", err)
	}

	// Load aggregator artifacts
	aggArtifacts := aggregator.Artifacts
	if err := aggArtifacts.LoadAll(); err != nil {
		return nil, fmt.Errorf("failed to load aggregator artifacts: %w", err)
	}

	// Decode the aggregator circuit definition
	log.Debugw("reading cicuit artifact",
		"circuit", "aggregator",
		"type", "ccs",
		"size", len(aggArtifacts.CircuitDefinition()),
	)
	aggCcs := groth16.NewCS(ecc.BW6_761)
	if _, err := aggCcs.ReadFrom(bytes.NewReader(aggArtifacts.CircuitDefinition())); err != nil {
		return nil, fmt.Errorf("failed to read aggregator circuit definition: %w", err)
	}

	// Decode the aggregator proving key
	log.Debugw("reading cicuit artifact",
		"circuit", "aggregator",
		"type", "pk",
		"size", len(aggArtifacts.ProvingKey()),
	)
	aggPk := groth16.NewProvingKey(ecc.BW6_761)
	if _, err := aggPk.ReadFrom(bytes.NewReader(aggArtifacts.ProvingKey())); err != nil {
		return nil, fmt.Errorf("failed to read aggregator proving key: %w", err)
	}

	log.Debugw("sequencer initialized",
		"batchTimeWindow", batchTimeWindow,
		"took(s)", time.Since(startTime).Seconds(),
	)

	return &Sequencer{
		stg:                          stg,
		maxTimeWindow:                batchTimeWindow,
		pids:                         make(map[string]time.Time),
		ballotVerifyingKeyCircomJSON: ballottest.TestCircomVerificationKey, // TODO: replace with a proper VK path
		aggregateProvingKey:          aggPk,
		aggregateCcs:                 aggCcs,
		voteProvingKey:               votePk,
		voteCcs:                      voteCcs,
	}, nil
}

// Start begins the ballot processing and aggregation routines.
// It creates a new context derived from the provided one and starts
// the background goroutines for processing ballots and aggregating them.
//
// Parameters:
//   - ctx: Parent context for controlling the sequencer's lifecycle
//
// Returns an error if either processor fails to start.
func (s *Sequencer) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}

	s.ctx, s.cancel = context.WithCancel(ctx)

	if err := s.startBallotProcessor(); err != nil {
		s.cancel() // Clean up if we fail to start completely
		return fmt.Errorf("failed to start ballot processor: %w", err)
	}

	if err := s.startAggregateProcessor(); err != nil {
		s.cancel() // Clean up if we fail to start completely
		return fmt.Errorf("failed to start aggregate processor: %w", err)
	}

	log.Infow("sequencer started successfully")
	return nil
}

// Stop gracefully shuts down the sequencer by canceling its context.
// This will cause all background goroutines to terminate.
// It's safe to call Stop multiple times.
func (s *Sequencer) Stop() error {
	if s.cancel != nil {
		s.cancel()
		log.Infow("sequencer stopped")
	}
	return nil
}
