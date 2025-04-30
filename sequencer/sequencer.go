// Package sequencer provides functionality for processing and aggregating ballots
// into batches with zero-knowledge proofs for efficient verification.
package sequencer

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/aggregator"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/statetransition"
	ballottest "github.com/vocdoni/vocdoni-z-sandbox/circuits/test/ballotproof"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/voteverifier"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/web3"
)

var (
	// AggregatorTickerInterval is the interval at which the aggregator will check for new ballots to process.
	// This value can be changed before starting the sequencer.
	AggregatorTickerInterval = 10 * time.Second

	// NewProcessMonitorInterval is the interval at which the sequencer will check for new processes to participate in.
	// This value can be changed before starting the sequencer.
	NewProcessMonitorInterval = 60 * time.Second

	// ParticipateInAllProcesses determines if the sequencer should process ballots from all processes that are registered.
	// This is a temporary flag to simplify testing and will be removed in the future. The Sequencer caller must somehow
	// decide which processes to participate in.
	ParticipateInAllProcesses = true
)

// Sequencer is a worker that takes verified ballots and aggregates them into a single proof.
// It processes ballots and creates batches of proofs for efficient verification.
type Sequencer struct {
	stg                *storage.Storage
	contracts          *web3.Contracts // web3 contracts for on-chain interaction
	ctx                context.Context
	cancel             context.CancelFunc
	pids               map[string]time.Time // Maps process IDs to their last update time
	pidsLock           sync.RWMutex         // Protects access to the pids map
	workInProgressLock sync.RWMutex         // Lock to block new work while processing a batch or a state transition

	ballotVerifyingKeyCircomJSON []byte // Verification key for ballot proofs

	statetransitionProvingKey groth16.ProvingKey          // Key for generating state transition proofs
	statetransitionCcs        constraint.ConstraintSystem // Constraint system for state transition proofs

	aggregateProvingKey groth16.ProvingKey          // Key for generating aggregate proofs
	aggregateCcs        constraint.ConstraintSystem // Constraint system for aggregate proofs

	voteProvingKey groth16.ProvingKey          // Key for generating vote proofs
	voteCcs        constraint.ConstraintSystem // Constraint system for vote proofs

	// batchTimeWindow is the maximum time window to wait for a batch to be processed.
	// If this time elapses, the batch will be processed even if not full.
	batchTimeWindow time.Duration
}

// New creates a new Sequencer instance that processes ballots and aggregates them into batches.
// It loads all necessary cryptographic artifacts for proof verification and generation.
//
// Parameters:
//   - stg: Storage instance for accessing ballots and other data
//   - batchTimeWindow: Maximum time to wait before processing a batch even if not full
//
// Returns a configured Sequencer instance or an error if initialization fails.
func New(stg *storage.Storage, contracts *web3.Contracts, batchTimeWindow time.Duration) (*Sequencer, error) {
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
	voteCcs := groth16.NewCS(circuits.VoteVerifierCurve)
	if _, err := voteCcs.ReadFrom(bytes.NewReader(vvArtifacts.CircuitDefinition())); err != nil {
		return nil, fmt.Errorf("failed to read vote verifier definition: %w", err)
	}

	// Decode the vote verifier proving key
	log.Debugw("reading cicuit artifact",
		"circuit", "voteVerifier",
		"type", "pk",
		"size", len(vvArtifacts.ProvingKey()),
	)
	votePk := groth16.NewProvingKey(circuits.VoteVerifierCurve)
	if _, err := votePk.UnsafeReadFrom(bytes.NewReader(vvArtifacts.ProvingKey())); err != nil {
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
	aggCcs := groth16.NewCS(circuits.AggregatorCurve)
	if _, err := aggCcs.ReadFrom(bytes.NewReader(aggArtifacts.CircuitDefinition())); err != nil {
		return nil, fmt.Errorf("failed to read aggregator circuit definition: %w", err)
	}

	// Decode the aggregator proving key
	log.Debugw("reading cicuit artifact",
		"circuit", "aggregator",
		"type", "pk",
		"size", len(aggArtifacts.ProvingKey()),
	)
	aggPk := groth16.NewProvingKey(circuits.AggregatorCurve)
	if _, err := aggPk.UnsafeReadFrom(bytes.NewReader(aggArtifacts.ProvingKey())); err != nil {
		return nil, fmt.Errorf("failed to read aggregator proving key: %w", err)
	}

	// Load statetransition artifacts
	sttArtifacts := statetransition.Artifacts
	if err := sttArtifacts.LoadAll(); err != nil {
		return nil, fmt.Errorf("failed to load statetransition artifacts: %w", err)
	}

	// Decode the statetransition circuit definition
	log.Debugw("reading cicuit artifact",
		"circuit", "statetransition",
		"type", "ccs",
		"size", len(sttArtifacts.CircuitDefinition()),
	)
	sttCcs := groth16.NewCS(circuits.StateTransitionCurve)
	if _, err := sttCcs.ReadFrom(bytes.NewReader(sttArtifacts.CircuitDefinition())); err != nil {
		return nil, fmt.Errorf("failed to read statetransition circuit definition: %w", err)
	}

	// Decode the statetransition proving key
	log.Debugw("reading cicuit artifact",
		"circuit", "statetransition",
		"type", "pk",
		"size", len(sttArtifacts.ProvingKey()),
	)
	sttPk := groth16.NewProvingKey(circuits.StateTransitionCurve)
	if _, err := sttPk.UnsafeReadFrom(bytes.NewReader(sttArtifacts.ProvingKey())); err != nil {
		return nil, fmt.Errorf("failed to read statetransition proving key: %w", err)
	}

	log.Debugw("sequencer initialized",
		"batchTimeWindow", batchTimeWindow,
		"took(s)", time.Since(startTime).Seconds(),
	)

	return &Sequencer{
		stg:                          stg,
		contracts:                    contracts,
		batchTimeWindow:              batchTimeWindow,
		pids:                         make(map[string]time.Time),
		ballotVerifyingKeyCircomJSON: ballottest.TestCircomVerificationKey, // TODO: replace with a proper VK path
		statetransitionProvingKey:    sttPk,
		statetransitionCcs:           sttCcs,
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

	if err := s.startAggregateProcessor(AggregatorTickerInterval); err != nil {
		s.cancel() // Clean up if we fail to start completely
		return fmt.Errorf("failed to start aggregate processor: %w", err)
	}

	if err := s.startStateTransitionProcessor(); err != nil {
		s.cancel() // Clean up if we fail to start completely
		return fmt.Errorf("failed to start state transition processor: %w", err)
	}

	if err := s.startOnchainProcessor(); err != nil {
		s.cancel() // Clean up if we fail to start completely
		return fmt.Errorf("failed to start on-chain processor: %w", err)
	}

	// Start monitoring for new processes
	go s.monitorNewProcesses(s.ctx, NewProcessMonitorInterval)

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

// monitorNewProcesses periodically checks for new processes and registers them with the sequencer.
func (s *Sequencer) monitorNewProcesses(ctx context.Context, tickerInterval time.Duration) {
	ticker := time.NewTicker(tickerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			procesList, err := s.stg.ListProcesses()
			if err != nil {
				log.Errorw(err, "failed to list processes")
				continue
			}
			for _, proc := range procesList {
				if ParticipateInAllProcesses && !s.ExistsProcessID(proc) {
					log.Infow("new process registered for sequencing", "processID", fmt.Sprintf("%x", proc))
					s.AddProcessID(proc)
				}
			}
		}
	}
}

// AddProcessID registers a process ID with the sequencer for ballot processing.
// Only ballots belonging to registered process IDs will be processed.
// If the process ID is already registered, this operation has no effect.
//
// Parameters:
//   - pid: The process ID to register
func (s *Sequencer) AddProcessID(pid []byte) {
	if len(pid) == 0 {
		log.Warnw("attempted to add empty process ID")
		return
	}

	s.pidsLock.Lock()
	defer s.pidsLock.Unlock()

	pidStr := string(pid)
	if _, exists := s.pids[pidStr]; exists {
		log.Debugw("process ID already registered", "processID", fmt.Sprintf("%x", pid))
		return
	}

	s.pids[pidStr] = time.Now()
	log.Infow("process ID registered for sequencing", "processID", fmt.Sprintf("%x", pid))
}

// DelProcessID unregisters a process ID from the sequencer.
// If the process ID is not registered, this operation has no effect.
//
// Parameters:
//   - pid: The process ID to unregister
func (s *Sequencer) DelProcessID(pid []byte) {
	if len(pid) == 0 {
		return
	}

	s.pidsLock.Lock()
	defer s.pidsLock.Unlock()

	if _, exists := s.pids[string(pid)]; exists {
		delete(s.pids, string(pid))
		log.Infow("process ID unregistered from sequencing", "processID", fmt.Sprintf("%x", pid))
	}
}

// ExistsProcessID checks if a process ID is registered with the sequencer.
func (s *Sequencer) ExistsProcessID(pid []byte) bool {
	if len(pid) == 0 {
		return false
	}

	s.pidsLock.RLock()
	defer s.pidsLock.RUnlock()

	_, exists := s.pids[string(pid)]
	return exists
}

// SetBatchTimeWindow sets the maximum time window to wait for a batch to be processed.
// If this time elapses, the batch will be processed even if not full.
func (s *Sequencer) SetBatchTimeWindow(window time.Duration) {
	s.batchTimeWindow = window
}
