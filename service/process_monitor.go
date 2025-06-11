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
	MonitorProcessFinalization(ctx context.Context, interval time.Duration) (<-chan *types.Process, error)
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
		return fmt.Errorf("failed to start process monitoring: %w", err)
	}

	finalizedProcChan, err := pm.contracts.MonitorProcessFinalization(ctx, pm.interval)
	if err != nil {
		pm.cancel = nil
		return fmt.Errorf("failed to start finalized process monitoring: %w", err)
	}

	go pm.monitorProcesses(ctx, newProcChan, finalizedProcChan)
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

func (pm *ProcessMonitor) monitorProcesses(ctx context.Context, newProcCh, endedProcCh <-chan *types.Process) {
	for {
		select {
		case <-ctx.Done():
			return
		case process := <-newProcCh:
			if _, err := pm.storage.Process(new(types.ProcessID).SetBytes(process.ID)); err == nil {
				continue
			}
			log.Debugw("new process found", "pid", process.ID.String())
			if err := pm.storage.NewProcess(process); err != nil {
				log.Warnw("failed to store new process", "pid", process.ID.String(), "err", err.Error())
			}
		case process := <-endedProcCh:
			log.Debugw("ended process found", "pid", process.ID.String())
			if err := pm.storage.UpdateProcess(process.ID, storage.ProcessUpdateCallbackSetStatus(process.Status)); err != nil {
				log.Warnw("failed to update process to ended status",
					"pid", process.ID.String(), "err", err.Error())
			}
		}
	}
}
