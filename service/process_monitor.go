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
	processIDVersion [4]byte
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
	CreateProcess(process *types.Process) (types.ProcessID, *common.Hash, error)
	Process(processID types.ProcessID) (*types.Process, error)
	ValidVersion(processID types.ProcessID) bool
	RegisterKnownProcess(processID types.ProcessID)
	AccountAddress() common.Address
	WaitTxByHash(hash common.Hash, timeout time.Duration, cb ...func(error)) error
	WaitTxByID(id []byte, timeout time.Duration, cb ...func(error)) error
}

// NewProcessMonitor creates a new ProcessMonitor service for one process ID
// version. If storage is nil, it uses a memory storage.
func NewProcessMonitor(contracts ContractsService, processIDVersion [4]byte, stg *storage.Storage, censusDownloader *CensusDownloader, stateSync *StateSync, interval time.Duration,
) *ProcessMonitor {
	if stg == nil {
		kv := memdb.New()
		stg = storage.New(kv)
	}
	return &ProcessMonitor{
		contracts:        contracts,
		processIDVersion: processIDVersion,
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
	processIDs, err := pm.storage.ListProcesses()
	if err != nil {
		return fmt.Errorf("failed to list processes: %w", err)
	}

	// Register each process ID in the contracts' knownProcesses map
	registeredCount := 0
	skippedCount := 0
	for _, processID := range processIDs {
		if !pm.contracts.ValidVersion(processID) {
			log.Warnw("unsupported process detected", "processID", processID.String())
			skippedCount++
			continue
		}
		if !pm.ownsProcess(processID) {
			skippedCount++
			continue
		}
		pm.contracts.RegisterKnownProcess(processID)
		registeredCount++
	}

	log.Infow("initialized known processes from storage",
		"registeredProcesses", registeredCount,
		"skippedProcesses", skippedCount,
		"processIDVersion", fmt.Sprintf("%x", pm.processIDVersion))

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
	processIDs, err := pm.storage.ListProcesses()
	if err != nil {
		return fmt.Errorf("failed to list processes: %w", err)
	}

	syncCount := 0
	skippedCount := 0
	for _, processID := range processIDs {
		if !pm.contracts.ValidVersion(processID) {
			log.Warnw("unsupported process detected", "processID", processID.String())
			skippedCount++
			continue
		}
		if !pm.ownsProcess(processID) {
			skippedCount++
			continue
		}
		// Check if process is accepting votes
		isAccepting, err := pm.storage.ProcessIsAcceptingVotes(processID)
		if err != nil || !isAccepting {
			continue
		}

		// Fetch current state from blockchain
		blockchainProcess, err := pm.contracts.Process(processID)
		if err != nil {
			log.Warnw("failed to fetch process from blockchain during sync",
				"processID", processID.String(), "error", err)
			continue
		}

		// Fetch from local storage
		localProcess, err := pm.storage.Process(processID)
		if err != nil {
			log.Warnw("failed to fetch process from storage during sync",
				"processID", processID.String(), "error", err)
			continue
		}

		// Compare and update if different
		if !localProcess.StateRoot.Equal(blockchainProcess.StateRoot) ||
			!localProcess.VotersCount.Equal(blockchainProcess.VotersCount) ||
			!localProcess.OverwrittenVotesCount.Equal(blockchainProcess.OverwrittenVotesCount) {
			// Use ProcessUpdateCallbackSetStateRoot to set absolute values from blockchain
			if err := pm.storage.UpdateProcess(processID,
				storage.ProcessUpdateCallbackSetStateRoot(
					blockchainProcess.StateRoot,
					blockchainProcess.VotersCount,
					blockchainProcess.OverwrittenVotesCount,
				)); err != nil {
				log.Warnw("failed to sync process from blockchain",
					"processID", processID.String(),
					"error", err)
				continue
			}

			log.Infow("synced process from blockchain",
				"processID", processID.String(),
				"stateRoot", blockchainProcess.StateRoot.String(),
				"votersCount", blockchainProcess.VotersCount.String(),
				"overwrittenVotesCount", blockchainProcess.OverwrittenVotesCount.String())
			syncCount++
		}
	}

	if syncCount > 0 {
		log.Infow("blockchain sync completed",
			"syncedProcesses", syncCount,
			"skippedProcesses", skippedCount,
			"totalProcesses", len(processIDs))
	}
	return nil
}

func (pm *ProcessMonitor) ownsProcess(processID types.ProcessID) bool {
	return processID.IsValid() && processID.Version() == pm.processIDVersion
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
		case process, ok := <-newProcChan:
			if !ok {
				log.Warn("process creation channel closed")
				return
			}
			if process == nil || process.ID == nil {
				log.Warn("received process creation event without process ID")
				continue
			}
			if !pm.contracts.ValidVersion(*process.ID) {
				log.Warn("received process creation event with invalid process ID (version mismatch)")
				continue
			}
			if !pm.ownsProcess(*process.ID) {
				log.Warnw("ignoring process creation for foreign runtime",
					"processID", process.ID.String(),
					"processIDVersion", fmt.Sprintf("%x", process.ID.Version()),
					"monitorProcessIDVersion", fmt.Sprintf("%x", pm.processIDVersion))
				continue
			}
			// Skip if the process already exists
			if _, err := pm.storage.Process(*process.ID); err == nil {
				continue
			}
			log.Debugw("new process found",
				"processID", process.ID.String(),
				"stateRoot", process.StateRoot.HexBytes().String())

			// Create a function to store the new process
			processSetup := func(p *types.Process) {
				if err := pm.storage.NewProcess(p); err != nil {
					log.Errorw(err, fmt.Sprintf("failed to store new process %s", p.ID.String()))
					return
				}
				log.Debugw("process created",
					"processID", p.ID.String(),
					"stateRoot", p.StateRoot.HexBytes().String(),
					"censusRoot", p.Census.CensusRoot.String())
			}

			// If the process is ready and has a census, download and import it
			// first, then store the process. If not, just store the process
			// directly.
			if process.Status == types.ProcessStatusReady && process.Census != nil {
				go func(process *types.Process) {
					// Keep one census copy for the async downloader queue and a
					// separate one for process state updates so the monitor does
					// not race with worker goroutines over the same struct.
					queuedCensus := process.Census.Clone()
					processCensus := process.Census.Clone()

					// Download and import the process census if needed
					resolvedRoot, err := pm.censusDownloader.DownloadCensus(*process.ID, queuedCensus)
					if err != nil {
						log.Warnw("failed to start census download for new process",
							"processID", process.ID.String(),
							"censusRoot", processCensus.CensusRoot.String(),
							"error", err.Error())
						return
					}
					processCensus.CensusRoot = resolvedRoot
					// After census is downloaded and imported, store the new process
					downloadCtx, downloadCtxCancel := context.WithTimeout(ctx, pm.censusDownloader.waitTimeout())
					pm.censusDownloader.OnCensusDownloaded(*process.ID, processCensus, downloadCtx, func(err error) {
						defer downloadCtxCancel()
						// If no error, just proceed to store the process.
						if err == nil {
							process.Census = processCensus
							processSetup(process)
							return
						}
						// If the initial census download fails, skip process
						// setup entirely. A process must not be created without
						// a successful initial census import.
						log.Warnw("failed to download census for new process",
							"processID", process.ID.String(),
							"censusRoot", processCensus.CensusRoot.String(),
							"error", err.Error())
					})
				}(process)
			} else {
				processSetup(process)
			}
		case update, ok := <-updatedProcChan:
			if !ok {
				log.Warn("process updates channel closed")
				return
			}
			if update == nil {
				log.Warn("received nil process update event")
				continue
			}
			if !pm.contracts.ValidVersion(update.ProcessID) {
				log.Warn("received process update event with invalid process ID (version mismatch)")
				continue
			}
			if !pm.ownsProcess(update.ProcessID) {
				log.Warnw("ignoring process update for foreign runtime",
					"processID", update.ProcessID.String(),
					"processIDVersion", fmt.Sprintf("%x", update.ProcessID.Version()),
					"monitorProcessIDVersion", fmt.Sprintf("%x", pm.processIDVersion))
				continue
			}
			// determine the type of update
			switch {
			case update.StatusChange != nil:
				// process status change
				log.Debugw("process changed status",
					"processID", update.ProcessID.String(),
					"old", update.OldStatus.String(),
					"new", update.NewStatus.String())
				if update.NewStatus == types.ProcessStatusResults {
					// For finalization, first fetch and store results, then
					// mark status as Results. Get the results from the
					// contract.
					process, err := pm.contracts.Process(update.ProcessID)
					if err != nil {
						log.Warnw("failed to fetch process from contract",
							"processID", update.ProcessID.String(),
							"error", err.Error())
						continue
					}
					// Ensure that results are actually present before updating
					// storage.
					if len(process.Result) == 0 {
						log.Warnw("process results not yet available; skipping finalization update",
							"processID", update.ProcessID.String())
						continue
					}
					if err := pm.storage.UpdateProcess(update.ProcessID, storage.ProcessUpdateCallbackFinalization(process.Result)); err != nil {
						log.Warnw("failed to update process results",
							"processID", update.ProcessID.String(),
							"error", err.Error())
						continue
					}
					// Clean up any stale votes
					if err := pm.storage.CleanProcessStaleVotes(update.ProcessID); err != nil {
						log.Warnw("failed to clean stale votes after process finalization",
							"processID", update.ProcessID.String(), "error", err.Error())
					}
				} else {
					// Just update the status if is not results
					if err := pm.storage.UpdateProcess(update.ProcessID, storage.ProcessUpdateCallbackSetStatus(
						update.NewStatus,
					)); err != nil {
						log.Warnw("failed to update process status",
							"processID", update.ProcessID.String(),
							"error", err.Error())
						continue
					}
				}
			case update.StateRootChange != nil:
				// process state root change
				log.Debugw("process state root changed",
					"processID", update.ProcessID.String(),
					"newStateRoot", update.NewStateRoot.String(),
					"newVotersCount", update.NewVotersCount.String(),
					"newOverwrittenVotesCount", update.NewOverwrittenVotesCount.String())
				if err := pm.storage.UpdateProcess(update.ProcessID, storage.ProcessUpdateCallbackSetStateRoot(
					update.NewStateRoot,
					update.NewVotersCount,
					update.NewOverwrittenVotesCount,
				)); err != nil {
					log.Errorw(err, fmt.Sprintf("failed to update process %s state root", update.ProcessID.String()))
					continue
				}
				// Notify StateSync service for blob fetching and state reconstruction (non-blocking)
				if pm.statesync != nil {
					pm.statesync.Notify(update)
				}

			case update.MaxVotersChange != nil:
				// process max voters change
				log.Debugw("process max voters changed",
					"processID", update.ProcessID.String(),
					"newMaxVoters", update.NewMaxVoters.String())
				if err := pm.storage.UpdateProcess(update.ProcessID, storage.ProcessUpdateCallbackSetMaxVoters(
					update.NewMaxVoters,
				)); err != nil {
					log.Warnw("failed to update process max voters",
						"processID", update.ProcessID.String(),
						"error", err.Error())
				}
			case update.CensusRootChange != nil:
				// fetch the process to get the current census info
				process, err := pm.storage.Process(update.ProcessID)
				if err != nil {
					log.Warnw("received update for unknown process",
						"processID", update.ProcessID.String(),
						"error", err.Error())
					continue
				}
				// process census root change
				log.Debugw("process census root or/and URI changed",
					"processID", update.ProcessID.String(),
					"newCensusRoot", update.NewCensusRoot.String(),
					"newCensusURI", update.NewCensusURI)
				newCensus := &types.Census{
					CensusOrigin:    process.Census.CensusOrigin,
					CensusRoot:      update.NewCensusRoot,
					CensusURI:       update.NewCensusURI,
					ContractAddress: process.Census.ContractAddress,
				}
				go func(censusInfo *types.Census, pid types.ProcessID, newRoot types.HexBytes, newURI string) {
					// download and import the new census
					censusInfo.CensusRoot, err = pm.censusDownloader.DownloadCensus(pid, censusInfo)
					if err != nil {
						log.Warnw("failed to start download of updated census for process",
							"processID", pid.String(),
							"censusRoot", newRoot.String(),
							"error", err.Error())
						return
					}
					// wait for census to be downloaded and imported, then update
					// process census info
					downloadCtx, downloadCtxCancel := context.WithTimeout(ctx, pm.censusDownloader.waitTimeout())
					pm.censusDownloader.OnCensusDownloaded(pid, censusInfo, downloadCtx, func(err error) {
						defer downloadCtxCancel()
						if err != nil {
							log.Warnw("failed to download updated census for process",
								"processID", pid.String(),
								"censusRoot", newRoot.String(),
								"error", err.Error())
							return
						}
						log.Debugw("new process census downloaded",
							"processID", pid.String(),
							"newCensusRoot", newRoot.String(),
							"newCensusURI", newURI)
						// update process census info in storage
						if err := pm.storage.UpdateProcess(pid, storage.ProcessUpdateCallbackSetCensusRoot(
							newRoot,
							newURI,
						)); err != nil {
							log.Warnw("failed to update process census root",
								"processID", pid.String(),
								"error", err.Error())
						}
						log.Infow("process census updated",
							"processID", pid.String(),
							"censusRoot", newRoot.String(),
							"censusURI", newURI)
					})
				}(newCensus, update.ProcessID, update.NewCensusRoot, update.NewCensusURI)
			}
		}
	}
}
