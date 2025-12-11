package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/types"
)

var _ ContractsService = &MockContracts{}

// MockContracts implements a mock version of web3.Contracts for testing
type MockContracts struct {
	processes []*types.Process
	mu        sync.Mutex
}

func NewMockContracts() *MockContracts {
	return &MockContracts{
		processes: make([]*types.Process, 0),
	}
}

func (m *MockContracts) MonitorProcessCreation(ctx context.Context, interval time.Duration) (<-chan *types.Process, error) {
	ch := make(chan *types.Process)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.mu.Lock()
				for _, proc := range m.processes {
					ch <- proc
				}
				m.processes = nil // Clear after sending
				m.mu.Unlock()
			}
		}
	}()
	return ch, nil
}

func (m *MockContracts) ProcessChangesFilters() []types.Web3FilterFn {
	return []types.Web3FilterFn{}
}

func (m *MockContracts) MonitorProcessChanges(
	ctx context.Context,
	interval time.Duration,
	retries int,
	filters ...types.Web3FilterFn,
) (<-chan *types.ProcessWithChanges, error) {
	return make(chan *types.ProcessWithChanges), nil
}

func (m *MockContracts) CreateProcess(process *types.Process) (*types.ProcessID, *common.Hash, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pid := types.ProcessID{
		Address: process.OrganizationId,
		Nonce:   uint64(len(m.processes)),
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}
	process.ID = pid.Marshal()
	m.processes = append(m.processes, process)
	hash := common.HexToHash("0x1234567890")
	return &pid, &hash, nil
}

func (m *MockContracts) AccountAddress() common.Address {
	return common.HexToAddress("0x1234567890123456789012345678901234567890")
}

func (m *MockContracts) WaitTxByHash(hash common.Hash, timeout time.Duration, cb ...func(error)) error {
	return nil
}

func (m *MockContracts) WaitTxByID(id []byte, timeout time.Duration, cb ...func(error)) error {
	return nil
}

func (m *MockContracts) Process(processID []byte) (*types.Process, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, proc := range m.processes {
		if string(proc.ID) == string(processID) {
			return proc, nil
		}
	}
	return nil, fmt.Errorf("process not found")
}

func (m *MockContracts) RegisterKnownProcess(processID string) {
	// No-op for mock
}
