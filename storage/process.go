package storage

import (
	"encoding/json"
	"fmt"

	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ethereum"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// Process retrieves the process data from the storage.
// It returns nil data and ErrNotFound if the metadata is not found.
func (s *Storage) Process(pid *types.ProcessID) (*types.Process, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	p := &types.Process{}
	if err := s.getArtifact(processPrefix, pid.Marshal(), p); err != nil {
		return nil, err
	}
	return p, nil
}

// SetProcess stores a process and its metadata into the storage.
func (s *Storage) SetProcess(data *types.Process) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	if data == nil {
		return fmt.Errorf("nil process data")
	}
	return s.setArtifact(processPrefix, data.ID, data)
}

// ListProcesses returns the list of process IDs stored in the storage (by SetProcessMetadata) as a list of byte slices.
func (s *Storage) ListProcesses() ([][]byte, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	pids, err := s.listArtifacts(processPrefix)
	if err != nil {
		return nil, err
	}
	return pids, nil
}

// MetadataHash returns the hash of the metadata.
func MetadataHash(metadata *types.Metadata) []byte {
	data, err := json.Marshal(metadata)
	if err != nil {
		panic(err)
	}
	return ethereum.HashRaw(data)
}
