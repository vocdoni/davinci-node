package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

var (
	_ ContractsService                = &MockContracts{}
	_ web3.BlobFetcher                = &MockContracts{}
	_ web3.ProcessBlobFetcherResolver = &MockContracts{}

	defaultMockProcessIDVersion = [4]byte{0x00, 0x00, 0x00, 0x01}
)

// MockContracts implements a mock version of web3.Contracts for testing
type MockContracts struct {
	processes       []*types.Process
	latestProcesses map[types.ProcessID]*types.Process
	blobs           map[common.Hash]*types.Blob
	activeProcesses map[types.ProcessID]struct{}
	processLookups  []types.ProcessID
	chanPWC         chan *types.ProcessWithChanges
	mu              sync.Mutex
}

func NewMockContracts() *MockContracts {
	return &MockContracts{
		processes:       make([]*types.Process, 0),
		latestProcesses: make(map[types.ProcessID]*types.Process),
		blobs:           make(map[common.Hash]*types.Blob),
		activeProcesses: make(map[types.ProcessID]struct{}),
		chanPWC:         make(chan *types.ProcessWithChanges),
	}
}

func (m *MockContracts) ProcessUpdatesFilters() []types.Web3FilterFn {
	return []types.Web3FilterFn{}
}

func (m *MockContracts) MonitorProcessUpdates(
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
		defaultMockProcessIDVersion,
		uint64(len(m.processes)),
	)
	process.ID = &processID
	creationProcess := cloneProcess(process)
	latestProcess := cloneProcess(process)
	m.processes = append(m.processes, creationProcess)
	m.latestProcesses[processID] = latestProcess
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

	m.processLookups = append(m.processLookups, processID)
	proc, ok := m.latestProcesses[processID]
	if ok {
		return cloneProcess(proc), nil
	}
	for _, proc := range m.processes {
		if proc == nil || proc.ID == nil {
			continue
		}
		if *proc.ID == processID {
			return cloneProcess(proc), nil
		}
	}
	return nil, fmt.Errorf("process not found")
}

func (m *MockContracts) ValidVersion(processID types.ProcessID) bool {
	return true
}

func (m *MockContracts) AddActiveProcess(processID types.ProcessID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeProcesses == nil {
		m.activeProcesses = make(map[types.ProcessID]struct{})
	}
	m.activeProcesses[processID] = struct{}{}
}

func (m *MockContracts) RemoveActiveProcess(processID types.ProcessID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.activeProcesses, processID)
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

func (m *MockContracts) BlobFetcherForProcess(_ types.ProcessID) (web3.BlobFetcher, error) {
	return m, nil
}

func (m *MockContracts) MockStateRootChange(_ context.Context, process *types.ProcessWithChanges) error {
	m.chanPWC <- process
	return nil
}

// SetLatestProcess replaces the latest on-chain snapshot for a process.
func (m *MockContracts) SetLatestProcess(process *types.Process) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if process == nil || process.ID == nil {
		return
	}
	m.latestProcesses[*process.ID] = cloneProcess(process)
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

func cloneProcess(process *types.Process) *types.Process {
	if process == nil {
		return nil
	}
	clone := *process
	if process.ID != nil {
		id := *process.ID
		clone.ID = &id
	}
	if process.StateRoot != nil {
		root := *process.StateRoot
		clone.StateRoot = &root
	}
	if process.EncryptionKey != nil {
		key := *process.EncryptionKey
		if process.EncryptionKey.X != nil {
			x := *process.EncryptionKey.X
			key.X = &x
		}
		if process.EncryptionKey.Y != nil {
			y := *process.EncryptionKey.Y
			key.Y = &y
		}
		clone.EncryptionKey = &key
	}
	if process.Census != nil {
		census := *process.Census
		clone.Census = &census
	}
	if process.MaxVoters != nil {
		maxVoters := *process.MaxVoters
		clone.MaxVoters = &maxVoters
	}
	if process.VotersCount != nil {
		votersCount := *process.VotersCount
		clone.VotersCount = &votersCount
	}
	if process.OverwrittenVotesCount != nil {
		overwrittenVotesCount := *process.OverwrittenVotesCount
		clone.OverwrittenVotesCount = &overwrittenVotesCount
	}
	if process.Result != nil {
		clone.Result = make([]*types.BigInt, len(process.Result))
		for i, r := range process.Result {
			if r == nil {
				continue
			}
			value := *r
			clone.Result[i] = &value
		}
	}
	return &clone
}
