package service

import (
	"context"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// MockContracts implements a mock version of web3.Contracts for testing
type MockContracts struct {
	processes []*types.Process
	chainID   uint64
	mu        sync.Mutex
}

func NewMockContracts() *MockContracts {
	return &MockContracts{
		processes: make([]*types.Process, 0),
		chainID:   1,
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

func (m *MockContracts) MonitorProcessFinalization(ctx context.Context, interval time.Duration) (<-chan *types.Process, error) {
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

func (m *MockContracts) CreateProcess(process *types.Process) (*types.ProcessID, *common.Hash, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pid := types.ProcessID{
		Address: process.OrganizationId,
		Nonce:   uint64(len(m.processes)),
		ChainID: uint32(m.chainID),
	}
	process.ID = pid.Marshal()
	m.processes = append(m.processes, process)
	hash := common.HexToHash("0x1234567890")
	return &pid, &hash, nil
}

func (m *MockContracts) AccountAddress() common.Address {
	return common.HexToAddress("0x1234567890123456789012345678901234567890")
}

func (m *MockContracts) WaitTx(hash common.Hash, timeout time.Duration) error {
	return nil
}
