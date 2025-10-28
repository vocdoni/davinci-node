package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	eth2deneb "github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/types"
)

var _ ContractsService = &MockContracts{}

// MockContracts implements a mock version of web3.Contracts for testing
type MockContracts struct {
	processes []*types.Process
	blobs     map[common.Hash]*eth2deneb.Blob

	chanStateRootChange chan *types.ProcessWithStateRootChange

	mu sync.Mutex
}

func NewMockContracts() *MockContracts {
	return &MockContracts{
		processes: make([]*types.Process, 0),
		blobs:     make(map[common.Hash]*eth2deneb.Blob),

		chanStateRootChange: make(chan *types.ProcessWithStateRootChange),
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

func (m *MockContracts) MonitorProcessStatusChanges(ctx context.Context, interval time.Duration) (<-chan *types.ProcessWithStatusChange, error) {
	return make(chan *types.ProcessWithStatusChange), nil
}

func (m *MockContracts) MonitorProcessStateRootChange(ctx context.Context, interval time.Duration) (<-chan *types.ProcessWithStateRootChange, error) {
	return m.chanStateRootChange, nil
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

func (m *MockContracts) BlobsByTxHash(ctx context.Context, txHash common.Hash,
) ([]*eth2deneb.BlobSidecar, error) {
	if blob, ok := m.blobs[txHash]; ok {
		return []*eth2deneb.BlobSidecar{{
			Blob: *blob,
		}}, nil
	}
	return nil, fmt.Errorf("txHash not found")
}

func (m *MockContracts) MockStateRootChange(_ context.Context, process *types.ProcessWithStateRootChange) error {
	m.chanStateRootChange <- process
	return nil
}

func (m *MockContracts) SendBlobTx(
	blob *eth2deneb.Blob,
) common.Hash {
	var txHash common.Hash
	_, _ = rand.Read(txHash[:])
	m.blobs[txHash] = blob
	return txHash
}
