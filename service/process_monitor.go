package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

// ProcessMonitor is a service that monitors new voting processes or process
// updates and update them in the local storage.
type ProcessMonitor struct {
	contracts        ContractsService
	storage          *storage.Storage
	censusDownloader *CensusDownloader
	statesync        *StateSync
	interval         time.Duration
	mu               sync.Mutex
	cancel           context.CancelFunc
}

// ContractsService defines the interface for web3 contract operations.
type ContractsService interface {
	MonitorProcessCreation(ctx context.Context, interval time.Duration) (<-chan *types.Process, error)
	ProcessChangesFilters() []types.Web3FilterFn
	MonitorProcessChanges(ctx context.Context, interval time.Duration, retries int, filters ...types.Web3FilterFn) (<-chan *types.ProcessWithChanges, error)
	CreateProcess(process *types.Process) (*types.ProcessID, *common.Hash, error)
	Process(processID []byte) (*types.Process, error)
	RegisterKnownProcess(processID string)
	AccountAddress() common.Address
	WaitTxByHash(hash common.Hash, timeout time.Duration, cb ...func(error)) error
	WaitTxByID(id []byte, timeout time.Duration, cb ...func(error)) error
	BlobsByTxHash(ctx context.Context, txHash common.Hash) ([]*types.BlobSidecar, error)
}

// NewProcessMonitor creates a new ProcessMonitor service. If storage is nil, it uses a memory storage.
func NewProcessMonitor(contracts ContractsService, stg *storage.Storage, censusDownloader *CensusDownloader, stateSync *StateSync, interval time.Duration,
) *ProcessMonitor {
	if stg == nil {
		kv := memdb.New()
		stg = storage.New(kv)
	}
	return &ProcessMonitor{
		contracts:        contracts,
		storage:          stg,
		censusDownloader: censusDownloader,
		statesync:        stateSync,
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

	updatedProcChan, err := pm.contracts.MonitorProcessChanges(ctx, pm.interval, 3, pm.contracts.ProcessChangesFilters()...)
	if err != nil {
		pm.cancel = nil
		return fmt.Errorf("failed to start monitor of process updates: %w", err)
	}

	go pm.monitorProcesses(ctx, newProcChan, updatedProcChan)
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
		pm.contracts.RegisterKnownProcess(pid.String())
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
				"pid", pid.String(), "error", err)
			continue
		}

		// Fetch from local storage
		localProcess, err := pm.storage.Process(pid)
		if err != nil {
			log.Warnw("failed to fetch process from storage during sync",
				"pid", pid.String(), "error", err)
			continue
		}

		// Compare and update if different
		needsUpdate := false
		if !localProcess.StateRoot.Equal(blockchainProcess.StateRoot) {
			needsUpdate = true
		}
		if !localProcess.VotersCount.Equal(blockchainProcess.VotersCount) {
			needsUpdate = true
		}
		if !localProcess.OverwrittenVotesCount.Equal(blockchainProcess.OverwrittenVotesCount) {
			needsUpdate = true
		}

		if needsUpdate {
			// Use ProcessUpdateCallbackSetStateRoot to set absolute values from blockchain
			if err := pm.storage.UpdateProcess(pid,
				storage.ProcessUpdateCallbackSetStateRoot(
					blockchainProcess.StateRoot,
					blockchainProcess.VotersCount,
					blockchainProcess.OverwrittenVotesCount,
				)); err != nil {
				log.Warnw("failed to sync process from blockchain",
					"pid", pid.String(),
					"err", err)
				continue
			}

			log.Infow("synced process from blockchain",
				"pid", pid.String(),
				"stateRoot", blockchainProcess.StateRoot.String(),
				"votersCount", blockchainProcess.VotersCount.String(),
				"overwrittenVotesCount", blockchainProcess.OverwrittenVotesCount.String())
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
	updatedProcChan <-chan *types.ProcessWithChanges,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case process := <-newProcChan:
			// Skip if the process already exists
			if _, err := pm.storage.Process(new(types.ProcessID).SetBytes(process.ID)); err == nil {
				continue
			}
			log.Debugw("new process found", "pid", process.ID.String())

			// Create a function to store the new process
			processSetup := func() {
				if err := pm.storage.NewProcess(process); err != nil {
					log.Warnw("failed to store new process",
						"pid", process.ID.String(),
						"err", err.Error())
				}
				log.Debugw("process created", "pid", process.ID.String(), "stateRoot", process.StateRoot.String())
			}

			// If the process is ready and has a census, download and import it
			// first, then store the process. If not, just store the process
			// directly.
			if process.Status == types.ProcessStatusReady && process.Census != nil {
				// Download and import the process census if needed
				pm.censusDownloader.DownloadQueue <- process.Census
				// After census is downloaded and imported, store the new process
				downloadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				pm.censusDownloader.OnCensusDownloaded(process.Census, downloadCtx, func(err error) {
					if err != nil {
						log.Warnw("failed to download census for new process",
							"pid", process.ID.String(),
							"censusRoot", process.Census.CensusRoot.String(),
							"err", err.Error())
					}
					processSetup()
				})
			} else {
				processSetup()
			}
		case update := <-updatedProcChan:
			// decode pid
			pid := new(types.ProcessID).SetBytes(update.ProcessID)

			// determine the type of update
			switch {
			case update.StatusChange != nil:
				// process status change
				log.Debugw("process changed status",
					"pid", pid.String(),
					"old", update.OldStatus.String(),
					"new", update.NewStatus.String())
				if err := pm.storage.UpdateProcess(pid, storage.ProcessUpdateCallbackSetStatus(
					update.NewStatus,
				)); err != nil {
					log.Warnw("failed to update process status",
						"pid", pid.String(),
						"err", err.Error())
				}
				if update.NewStatus == types.ProcessStatusResults {
					if err := pm.storage.CleanProcessStaleVotes(pid.Marshal()); err != nil {
						log.Warnw("failed to clean stale votes after process finalization",
							"pid", pid.String(), "err", err.Error())
					}
				}
			case update.StateRootChange != nil:
				// process state root change
				log.Debugw("process state root changed",
					"pid", pid.String(),
					"newStateRoot", update.NewStateRoot.String(),
					"newVotersCount", update.NewVotersCount.String(),
					"newOverwrittenVotesCount", update.NewOverwrittenVotesCount.String())
				if err := pm.storage.UpdateProcess(pid, storage.ProcessUpdateCallbackSetStateRoot(
					update.NewStateRoot,
					update.NewVotersCount,
					update.NewOverwrittenVotesCount,
				)); err != nil {
					log.Warnw("failed to update process state root",
						"pid", pid.String(),
						"err", err.Error())
				}
				// Notify StateSync service for blob fetching and state reconstruction (non-blocking)
				if pm.statesync != nil {
					pm.statesync.Notify(update)
				}

			case update.MaxVotersChange != nil:
				// process max voters change
				log.Debugw("process max voters changed",
					"pid", pid.String(),
					"newMaxVoters", update.NewMaxVoters.String())
				if err := pm.storage.UpdateProcess(pid, storage.ProcessUpdateCallbackSetMaxVoters(
					update.NewMaxVoters,
				)); err != nil {
					log.Warnw("failed to update process max voters",
						"pid", pid.String(),
						"err", err.Error())
				}
			case update.CensusRootChange != nil:
				// fetch the process to get the current census info
				process, err := pm.storage.Process(pid)
				if err != nil {
					log.Warnw("received update for unknown process",
						"pid", pid.String(),
						"err", err.Error())
					continue
				}
				// process census root change
				log.Debugw("process census root or/and URI changed",
					"pid", pid.String(),
					"newCensusRoot", update.NewCensusRoot.String(),
					"newCensusURI", update.NewCensusURI)
				newCensus := &types.Census{
					CensusOrigin: process.Census.CensusOrigin,
					CensusRoot:   update.NewCensusRoot,
					CensusURI:    update.NewCensusURI,
				}
				pm.censusDownloader.DownloadQueue <- newCensus
				// wait for census to be downloaded and imported, then update
				// process census info
				pm.censusDownloader.OnCensusDownloaded(newCensus, ctx, func(err error) {
					if err != nil {
						log.Warnw("failed to download updated census for process",
							"pid", pid.String(),
							"censusRoot", update.NewCensusRoot.String(),
							"err", err.Error())
						return
					}
					log.Debugw("new process census downloaded",
						"pid", pid.String(),
						"newCensusRoot", update.NewCensusRoot.String(),
						"newCensusURI", update.NewCensusURI)
					// update process census info in storage
					if err := pm.storage.UpdateProcess(pid, storage.ProcessUpdateCallbackSetCensusRoot(
						update.NewCensusRoot,
						update.NewCensusURI,
					)); err != nil {
						log.Warnw("failed to update process census root",
							"pid", pid.String(),
							"err", err.Error())
					}
					log.Infow("process census updated",
						"pid", pid.String(),
						"censusRoot", update.NewCensusRoot.String(),
						"censusURI", update.NewCensusURI)
				})
			}
		}
	}
}
