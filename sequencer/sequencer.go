// Package sequencer provides functionality for processing and aggregating ballots
// into batches with zero-knowledge proofs for efficient verification.
package sequencer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
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
	NewProcessMonitorInterval = 10 * time.Second

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
	pids               *ProcessIDMap // Maps process IDs to their last update time
	workInProgressLock sync.RWMutex  // Lock to block new work while processing a batch or a state transition
	prover             ProverFunc    // Function for generating zero-knowledge proofs

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
	if err := voteverifier.Artifacts.LoadAll(); err != nil {
		return nil, fmt.Errorf("failed to load vote verifier artifacts: %w", err)
	}

	// Decode the vote verifier circuit definition
	log.Debugw("reading cicuit artifact",
		"circuit", "voteVerifier",
		"type", "ccs")
	voteCcs, err := voteverifier.Artifacts.CircuitDefinition()
	if err != nil {
		return nil, fmt.Errorf("failed to read vote verifier definition: %w", err)
	}
	// Decode the vote verifier proving key
	log.Debugw("reading cicuit artifact",
		"circuit", "voteVerifier",
		"type", "pk")
	votePk, err := voteverifier.Artifacts.ProvingKey()
	if err != nil {
		return nil, fmt.Errorf("failed to read vote verifier proving key: %w", err)
	}

	// Load aggregator artifacts
	if err := aggregator.Artifacts.LoadAll(); err != nil {
		return nil, fmt.Errorf("failed to load aggregator artifacts: %w", err)
	}

	// Decode the aggregator circuit definition
	log.Debugw("reading cicuit artifact",
		"circuit", "aggregator",
		"type", "ccs")
	aggCcs, err := aggregator.Artifacts.CircuitDefinition()
	if err != nil {
		return nil, fmt.Errorf("failed to read aggregator circuit definition: %w", err)
	}

	// Decode the aggregator proving key
	log.Debugw("reading cicuit artifact",
		"circuit", "aggregator",
		"type", "pk")
	aggPk, err := aggregator.Artifacts.ProvingKey()
	if err != nil {
		return nil, fmt.Errorf("failed to read aggregator proving key: %w", err)
	}

	// Load statetransition artifacts
	if err := statetransition.Artifacts.LoadAll(); err != nil {
		return nil, fmt.Errorf("failed to load statetransition artifacts: %w", err)
	}

	// Decode the statetransition circuit definition
	log.Debugw("reading cicuit artifact",
		"circuit", "statetransition",
		"type", "ccs")
	stCcs, err := statetransition.Artifacts.CircuitDefinition()
	if err != nil {
		return nil, fmt.Errorf("failed to read statetransition circuit definition: %w", err)
	}

	// Decode the statetransition proving key
	log.Debugw("reading cicuit artifact",
		"circuit", "statetransition",
		"type", "pk")
	stPk, err := statetransition.Artifacts.ProvingKey()
	if err != nil {
		return nil, fmt.Errorf("failed to read statetransition proving key: %w", err)
	}

	log.Debugw("sequencer initialized",
		"batchTimeWindow", batchTimeWindow.String(),
		"took(s)", time.Since(startTime).Seconds())
	return &Sequencer{
		stg:                          stg,
		contracts:                    contracts,
		batchTimeWindow:              batchTimeWindow,
		pids:                         NewProcessIDMap(),
		ballotVerifyingKeyCircomJSON: ballottest.TestCircomVerificationKey, // TODO: replace with a proper VK path
		statetransitionProvingKey:    stPk,
		statetransitionCcs:           stCcs,
		aggregateProvingKey:          aggPk,
		aggregateCcs:                 aggCcs,
		voteProvingKey:               votePk,
		voteCcs:                      voteCcs,
		prover:                       DefaultProver, // Use the default prover by default
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

// monitorNewProcesses checks for new processes immediately and then periodically registers them with the sequencer.
func (s *Sequencer) monitorNewProcesses(ctx context.Context, tickerInterval time.Duration) {
	// Check for processes immediately at startup
	s.checkAndRegisterProcesses()

	// Set up ticker for periodic checks
	ticker := time.NewTicker(tickerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkAndRegisterProcesses()
		}
	}
}

// checkAndRegisterProcesses fetches the list of processes and registers new ones with the sequencer.
func (s *Sequencer) checkAndRegisterProcesses() {
	procesList, err := s.stg.ListProcesses()
	if err != nil {
		log.Errorw(err, "failed to list processes")
		return
	}

	for _, proc := range procesList {
		if ParticipateInAllProcesses && !s.ExistsProcessID(proc) {
			log.Infow("new process registered for sequencing", "processID", fmt.Sprintf("%x", proc))
			s.AddProcessID(proc)
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
	if s.pids.Add(pid) {
		log.Infow("process ID registered for sequencing", "processID", fmt.Sprintf("%x", pid))
	}
}

// DelProcessID unregisters a process ID from the sequencer.
// If the process ID is not registered, this operation has no effect.
//
// Parameters:
//   - pid: The process ID to unregister
func (s *Sequencer) DelProcessID(pid []byte) {
	if s.pids.Remove(pid) {
		log.Infow("process ID unregistered from sequencing", "processID", fmt.Sprintf("%x", pid))
	}
}

// ExistsProcessID checks if a process ID is registered with the sequencer.
func (s *Sequencer) ExistsProcessID(pid []byte) bool {
	return s.pids.Exists(pid)
}

// SetBatchTimeWindow sets the maximum time window to wait for a batch to be processed.
// If this time elapses, the batch will be processed even if not full.
func (s *Sequencer) SetBatchTimeWindow(window time.Duration) {
	s.batchTimeWindow = window
}
