// Package sequencer provides functionality for processing and aggregating ballots
// into batches with zero-knowledge proofs for efficient verification.
package sequencer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
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
	*internalCircuits                   // Internal circuit artifacts for proof generation and verification
	*finalizer                          // Finalizer for handling process finalization
	stg                *storage.Storage // Storage instance for accessing ballots and other data
	contracts          *web3.Contracts  // web3 contracts for on-chain interaction
	ctx                context.Context
	cancel             context.CancelFunc
	pids               *ProcessIDMap // Maps process IDs to their last update time
	workInProgressLock sync.RWMutex  // Lock to block new work while processing a batch or a state transition
	prover             ProverFunc    // Function for generating zero-knowledge proofs
	// batchTimeWindow is the maximum time window to wait for a batch to be processed.
	// If this time elapses, the batch will be processed even if not full.
	batchTimeWindow time.Duration

	// Worker mode fields
	masterURL     string // URL of master node (empty for master mode)
	workerAddress string // Ethereum address identifying this worker
}

// New creates a new Sequencer instance that processes ballots and aggregates them into batches.
// It loads all necessary cryptographic artifacts for proof verification and generation.
//
// Parameters:
//   - stg: Storage instance for accessing ballots and other data
//   - contracts: Web3 contracts for on-chain interaction
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

	log.Debugw("sequencer initialized",
		"batchTimeWindow", batchTimeWindow.String(),
		"took", time.Since(startTime).String(),
	)
	// Create a new Sequencer instance
	s := &Sequencer{
		stg:             stg,
		contracts:       contracts,
		batchTimeWindow: batchTimeWindow,
		pids:            NewProcessIDMap(),
		prover:          DefaultProver,
	}
	// Load the internal circuits
	if err := s.loadInternalCircuitArtifacts(); err != nil {
		return nil, fmt.Errorf("failed to load internal circuit artifacts: %w", err)
	}
	// Initialize the finalizer
	s.finalizer = newFinalizer(stg, stg.StateDB(), s.internalCircuits, s.prover)
	return s, nil
}

// Start begins the ballot processing and aggregation routines.
// It creates a new context derived from the provided one and starts
// the background goroutines for processing ballots and aggregating them.
// In worker mode, it only starts the worker processor.
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

	if s.masterURL != "" {
		// Worker mode: start worker processor
		if err := s.startWorkerProcessor(); err != nil {
			s.cancel()
			return fmt.Errorf("failed to start worker processor: %w", err)
		}
		log.Infow("sequencer started in worker mode",
			"master", s.masterURL,
			"address", s.workerAddress)
		return nil
	}

	// Start monitoring for new processes
	s.monitorNewProcesses(s.ctx, NewProcessMonitorInterval)

	// Master mode: start all processors
	s.finalizer.Start(s.ctx, time.Minute)

	if err := s.startBallotProcessor(); err != nil {
		s.cancel()
		return fmt.Errorf("failed to start ballot processor: %w", err)
	}

	if err := s.startAggregateProcessor(AggregatorTickerInterval); err != nil {
		s.cancel()
		return fmt.Errorf("failed to start aggregate processor: %w", err)
	}

	if err := s.startStateTransitionProcessor(); err != nil {
		s.cancel()
		return fmt.Errorf("failed to start state transition processor: %w", err)
	}

	if err := s.startOnchainProcessor(); err != nil {
		s.cancel()
		return fmt.Errorf("failed to start on-chain processor: %w", err)
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

// monitorNewProcesses checks for new processes immediately and then periodically registers them with the sequencer.
func (s *Sequencer) monitorNewProcesses(ctx context.Context, tickerInterval time.Duration) {
	// Check for processes immediately at startup
	s.checkAndRegisterProcesses()

	go func() {
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
	}()
}

// checkAndRegisterProcesses fetches the list of processes and registers new ones with the sequencer.
func (s *Sequencer) checkAndRegisterProcesses() {
	procesList, err := s.stg.ListProcesses()
	if err != nil {
		log.Errorw(err, "failed to list processes")
		return
	}

	for _, pid := range procesList {
		proc, err := s.stg.Process(new(types.ProcessID).SetBytes(pid)) // Ensure the process is loaded in storage
		if err != nil {
			log.Warnw("failed to get process for registration", "processID", fmt.Sprintf("%x", pid), "error", err)
			continue
		}
		if s.ExistsProcessID(pid) && proc.Status != types.ProcessStatusReady {
			log.Infow("process deregistered from sequencing", "processID", fmt.Sprintf("%x", pid))
			s.DelProcessID(pid) // Unregister if the process
			continue
		}
		if ParticipateInAllProcesses && !s.ExistsProcessID(pid) && proc.Status == types.ProcessStatusReady {
			log.Infow("new process registered for sequencing", "processID", fmt.Sprintf("%x", pid))
			s.AddProcessID(pid)
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
	if err := s.stg.UpdateProcess(pid, storage.ProcessUpdateCallbackAcceptingVotes(true)); err != nil {
		log.Warnw("failed to set process accepting votes", "processID", fmt.Sprintf("%x", pid), "error", err)
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
	if err := s.stg.UpdateProcess(pid, storage.ProcessUpdateCallbackAcceptingVotes(false)); err != nil {
		log.Warnw("failed to set process not accepting votes", "processID", fmt.Sprintf("%x", pid), "error", err)
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

// ActiveProcessIDs returns a list of process IDs that are currently being tracked
// by the sequencer.
func (s *Sequencer) ActiveProcessIDs() [][]byte {
	return s.pids.List()
}
