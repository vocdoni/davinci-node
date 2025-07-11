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

// ProcessMonitor is a service that monitors new voting processes
// and stores them in the local storage.
type ProcessMonitor struct {
	contracts ContractsService
	storage   *storage.Storage
	interval  time.Duration
	mu        sync.Mutex
	cancel    context.CancelFunc
}

// ContractsService defines the interface for web3 contract operations.
type ContractsService interface {
	MonitorProcessCreation(ctx context.Context, interval time.Duration) (<-chan *types.Process, error)
	MonitorProcessStatusChanges(ctx context.Context, interval time.Duration) (<-chan *types.ProcessWithStatusChange, error)
	MonitorProcessStateRootChange(ctx context.Context, interval time.Duration) (<-chan *types.ProcessWithStateRootChange, error)
	CreateProcess(process *types.Process) (*types.ProcessID, *common.Hash, error)
	AccountAddress() common.Address
	WaitTx(hash common.Hash, timeout time.Duration) error
}

// NewProcessMonitor creates a new ProcessMonitor service. If storage is nil, it uses a memory storage.
func NewProcessMonitor(contracts ContractsService, stg *storage.Storage, interval time.Duration) *ProcessMonitor {
	if stg == nil {
		kv := memdb.New()
		stg = storage.New(kv)
	}
	return &ProcessMonitor{
		contracts: contracts,
		storage:   stg,
		interval:  interval,
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
			// if it does not exist, create a new one
			log.Debugw("new process found", "pid", process.ID.String())
			if err := pm.storage.NewProcess(process); err != nil {
				log.Warnw("failed to store new process", "pid", process.ID.String(), "err", err.Error())
			}
			// initialize the state for the process
			log.Debugw("process state created", "pid", process.ID.String())
		case process := <-changedStatusProcChan:
			log.Debugw("process changed status", "pid", process.ID.String(),
				"old", process.OldStatus.String(), "new", process.NewStatus.String())
			if err := pm.storage.UpdateProcess(process.ID, storage.ProcessUpdateCallbackSetStatus(process.Status)); err != nil {
				log.Warnw("failed to update process status",
					"pid", process.ID.String(), "err", err.Error())
			}
		case process := <-stateTransitionChan:
			log.Debugw("process state root changed", "pid", process.ID.String(),
				"stateRoot", process.NewStateRoot.String(),
				"voteCount", process.NewVoteCount.String(),
				"voteOverwrittenCount", process.NewVoteOverwrittenCount.String())
			if err := pm.storage.UpdateProcess(process.ID,
				storage.ProcessUpdateCallbackSetStateRoot(process.NewStateRoot,
					process.NewVoteCount, process.NewVoteOverwrittenCount)); err != nil {
				log.Warnw("failed to update process state root",
					"pid", process.ID.String(), "err", err.Error())
			}
		}
	}
}
