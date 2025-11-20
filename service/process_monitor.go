package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/census"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

// ProcessMonitor is a service that monitors new voting processes or process
// updates and update them in the local storage.
type ProcessMonitor struct {
	contracts        ContractsService
	storage          *storage.Storage
	censusDownloader *census.CensusImporter
	interval         time.Duration
	mu               sync.Mutex
	cancel           context.CancelFunc
}

// ContractsService defines the interface for web3 contract operations.
type ContractsService interface {
	MonitorProcessCreation(ctx context.Context, interval time.Duration) (<-chan *types.Process, error)
	MonitorProcessStatusChanges(ctx context.Context, interval time.Duration) (<-chan *types.ProcessWithStatusChange, error)
	MonitorProcessStateRootChange(ctx context.Context, interval time.Duration) (<-chan *types.ProcessWithStateRootChange, error)
	CreateProcess(process *types.Process) (*types.ProcessID, *common.Hash, error)
	Process(processID []byte) (*types.Process, error)
	RegisterKnownProcess(processID string)
	AccountAddress() common.Address
	WaitTxByHash(hash common.Hash, timeout time.Duration, cb ...func(error)) error
	WaitTxByID(id []byte, timeout time.Duration, cb ...func(error)) error
}

// NewProcessMonitor creates a new ProcessMonitor service. If storage is nil, it uses a memory storage.
func NewProcessMonitor(contracts ContractsService, stg *storage.Storage, interval time.Duration) *ProcessMonitor {
	if stg == nil {
		kv := memdb.New()
		stg = storage.New(kv)
	}
	return &ProcessMonitor{
		contracts:        contracts,
		storage:          stg,
		censusDownloader: census.NewCensusImporter(stg),
		interval:         interval,
	}
}

// Start begins monitoring for new processes. It returns an error if the service
// is already running or if it fails to start monitoring.
func (pm *ProcessMonitor) Start(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.cancel != nil {
		return fmt.Errorf("service already running")
	}

	// Initialize known processes from storage before starting monitors
	if err := pm.initializeKnownProcesses(); err != nil {
		return fmt.Errorf("failed to initialize known processes: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	pm.cancel = cancel

	newProcChan, err := pm.contracts.MonitorProcessCreation(ctx, pm.interval)
	if err != nil {
		pm.cancel = nil
		return fmt.Errorf("failed to start monitor of process creation: %w", err)
	}

	changedStatusProcChan, err := pm.contracts.MonitorProcessStatusChanges(ctx, pm.interval)
	if err != nil {
		pm.cancel = nil
		return fmt.Errorf("failed to start monitor of process status changes: %w", err)
	}

	stateTransitionChan, err := pm.contracts.MonitorProcessStateRootChange(ctx, pm.interval)
	if err != nil {
		pm.cancel = nil
		return fmt.Errorf("failed to start monitor of process state root changes: %w", err)
	}

	go pm.monitorProcesses(ctx, newProcChan, changedStatusProcChan, stateTransitionChan)
	return nil
}

// Stop halts the monitoring service.
func (pm *ProcessMonitor) Stop() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.cancel != nil {
		pm.cancel()
		pm.cancel = nil
	}
}

// initializeKnownProcesses loads all existing process IDs from storage and
// registers them in the contracts' knownProcesses map. This ensures that after
// a restart, state transition events for existing processes are not filtered out.
// It also syncs active processes from the blockchain to catch up on any missed
// state transitions.
func (pm *ProcessMonitor) initializeKnownProcesses() error {
	// Get all process IDs from storage
	pids, err := pm.storage.ListProcesses()
	if err != nil {
		return fmt.Errorf("failed to list processes: %w", err)
	}

	// Register each process ID in the contracts' knownProcesses map
	for _, pid := range pids {
		processID := fmt.Sprintf("%x", pid)
		pm.contracts.RegisterKnownProcess(processID)
	}

	log.Infow("initialized known processes from storage", "count", len(pids))

	// Sync active processes from blockchain to catch up on missed state transitions
	if err := pm.syncActiveProcessesFromBlockchain(); err != nil {
		log.Warnw("failed to sync processes from blockchain", "error", err)
		// Don't fail startup - log warning and continue
	}

	return nil
}

// syncActiveProcessesFromBlockchain fetches current state from blockchain for
// all processes that are accepting votes. This ensures that after a restart,
// any missed state transitions are reflected in local storage.
func (pm *ProcessMonitor) syncActiveProcessesFromBlockchain() error {
	pids, err := pm.storage.ListProcesses()
	if err != nil {
		return fmt.Errorf("failed to list processes: %w", err)
	}

	syncCount := 0
	for _, pid := range pids {
		// Check if process is accepting votes
		isAccepting, err := pm.storage.ProcessIsAcceptingVotes(pid)
		if err != nil || !isAccepting {
			continue
		}

		// Fetch current state from blockchain
		blockchainProcess, err := pm.contracts.Process(pid.Marshal())
		if err != nil {
			log.Warnw("failed to fetch process from blockchain during sync",
				"pid", fmt.Sprintf("%x", pid), "error", err)
			continue
		}

		// Fetch from local storage
		localProcess, err := pm.storage.Process(pid)
		if err != nil {
			log.Warnw("failed to fetch process from storage during sync",
				"pid", fmt.Sprintf("%x", pid), "error", err)
			continue
		}

		// Compare and update if different
		needsUpdate := false
		if !localProcess.StateRoot.Equal(blockchainProcess.StateRoot) {
			needsUpdate = true
		}
		if !localProcess.VoteCount.Equal(blockchainProcess.VoteCount) {
			needsUpdate = true
		}
		if !localProcess.VoteOverwrittenCount.Equal(blockchainProcess.VoteOverwrittenCount) {
			needsUpdate = true
		}

		if needsUpdate {
			// Use ProcessUpdateCallbackSetStateRoot to set absolute values from blockchain
			if err := pm.storage.UpdateProcess(pid,
				storage.ProcessUpdateCallbackSetStateRoot(
					blockchainProcess.StateRoot,
					blockchainProcess.VoteCount,
					blockchainProcess.VoteOverwrittenCount,
				)); err != nil {
				log.Warnw("failed to sync process from blockchain",
					"pid", fmt.Sprintf("%x", pid), "error", err)
				continue
			}

			log.Infow("synced process from blockchain",
				"pid", fmt.Sprintf("%x", pid),
				"stateRoot", blockchainProcess.StateRoot.String(),
				"voteCount", blockchainProcess.VoteCount.String(),
				"overwrittenCount", blockchainProcess.VoteOverwrittenCount.String())
			syncCount++
		}
	}

	if syncCount > 0 {
		log.Infow("blockchain sync completed",
			"syncedProcesses", syncCount,
			"totalProcesses", len(pids))
	}
	return nil
}

func (pm *ProcessMonitor) monitorProcesses(
	ctx context.Context,
	newProcChan <-chan *types.Process,
	changedStatusProcChan <-chan *types.ProcessWithStatusChange,
	stateTransitionChan <-chan *types.ProcessWithStateRootChange,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case process := <-newProcChan:
			// try to update the process if it already exists
			if _, err := pm.storage.Process(new(types.ProcessID).SetBytes(process.ID)); err == nil {
				continue
			}
			// download and import the process census if needed
			if err := pm.censusDownloader.ImportCensus(ctx, process.Census); err != nil {
				log.Warnw("failed to download census for new process",
					"pid", process.ID.String(),
					"censusOrigin", process.Census.CensusOrigin.String(),
					"censusURI", process.Census.CensusURI,
					"err", err.Error())
			}
			// if it does not exist, create a new one
			log.Debugw("new process found", "pid", process.ID.String())
			if err := pm.storage.NewProcess(process); err != nil {
				log.Warnw("failed to store new process",
					"pid", process.ID.String(),
					"err", err.Error())
			}
			// initialize the state for the process
			log.Debugw("process state created", "pid", process.ID.String())
		case process := <-changedStatusProcChan:
			log.Debugw("process changed status",
				"pid", process.ID.String(),
				"old", process.OldStatus.String(),
				"new", process.NewStatus.String())
			if err := pm.storage.UpdateProcess(
				new(types.ProcessID).SetBytes(process.ID),
				storage.ProcessUpdateCallbackSetStatus(process.Status),
			); err != nil {
				log.Warnw("failed to update process status",
					"pid", process.ID.String(),
					"err", err.Error())
			}
			if process.Status == types.ProcessStatusResults {
				if err := pm.storage.CleanProcessStaleVotes(process.ID); err != nil {
					log.Warnw("failed to clean stale votes after process finalization",
						"pid", process.ID.String(), "err", err.Error())
				}
			}
		case process := <-stateTransitionChan:
			log.Debugw("process state root changed",
				"pid", process.ID.String(),
				"stateRoot", process.NewStateRoot.String(),
				"voteCount", process.NewVoteCount.String(),
				"voteOverwrittenCount", process.NewVoteOverwrittenCount.String())
			if err := pm.storage.UpdateProcess(
				new(types.ProcessID).SetBytes(process.ID),
				storage.ProcessUpdateCallbackSetStateRoot(
					process.NewStateRoot,
					process.NewVoteCount,
					process.NewVoteOverwrittenCount,
				),
			); err != nil {
				log.Warnw("failed to update process state root",
					"pid", process.ID.String(),
					"err", err.Error())
			}
		}
	}
}
