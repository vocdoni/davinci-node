package service

import (
	"context"
	"crypto/rand"
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
	blobs     map[common.Hash]*types.Blob
	chanPWC   chan *types.ProcessWithChanges
	mu        sync.Mutex
}

func NewMockContracts() *MockContracts {
	return &MockContracts{
		processes: make([]*types.Process, 0),
		blobs:     make(map[common.Hash]*types.Blob),
		chanPWC:   make(chan *types.ProcessWithChanges),
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
	return m.chanPWC, nil
}

func (m *MockContracts) CreateProcess(process *types.Process) (types.ProcessID, *common.Hash, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	processID := types.NewProcessID(
		process.OrganizationID,
		[4]byte{0x00, 0x00, 0x00, 0x01},
		uint64(len(m.processes)),
	)
	process.ID = &processID
	m.processes = append(m.processes, process)
	hash := common.HexToHash("0x1234567890")
	return processID, &hash, nil
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

func (m *MockContracts) Process(processID types.ProcessID) (*types.Process, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, proc := range m.processes {
		if *proc.ID == processID {
			return proc, nil
		}
	}
	return nil, fmt.Errorf("process not found")
}

func (m *MockContracts) RegisterKnownProcess(processID types.ProcessID) {
	// No-op for mock
}

func (m *MockContracts) BlobsByTxHash(ctx context.Context, txHash common.Hash,
) ([]*types.BlobSidecar, error) {
	if blob, ok := m.blobs[txHash]; ok {
		return []*types.BlobSidecar{{
			Blob: blob,
		}}, nil
	}
	return []*types.BlobSidecar{}, nil
}

func (m *MockContracts) MockStateRootChange(_ context.Context, process *types.ProcessWithChanges) error {
	m.chanPWC <- process
	return nil
}

func (m *MockContracts) SendBlobTx(blob []byte) common.Hash {
	var txHash common.Hash
	_, _ = rand.Read(txHash[:])
	m.blobs[txHash] = types.MustBlobFromBytes(blob)
	return txHash
}

func (m *MockContracts) FetchOnchainCensusRoot(address common.Address) (types.HexBytes, error) {
	return nil, fmt.Errorf("not implemented in mock")
}
