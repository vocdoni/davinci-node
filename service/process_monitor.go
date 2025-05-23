package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// ProcessMonitor represents a service that monitors new voting processes
// and stores them in the storage queue.
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

	go pm.monitorProcesses(ctx, newProcChan)
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

func (pm *ProcessMonitor) monitorProcesses(ctx context.Context, newProcChan <-chan *types.Process) {
	for {
		select {
		case <-ctx.Done():
			return
		case proc := <-newProcChan:
			if _, err := pm.storage.Process(new(types.ProcessID).SetBytes(proc.ID)); err == nil {
				// Process already exists
				continue
			}
			log.Debugw("new process found", "processID", proc.ID.String())
			if err := pm.storage.SetProcess(proc); err != nil {
				log.Warnw("failed to store process", "processID", proc.ID.String(), "error", err.Error())
			}
		}
	}
}
