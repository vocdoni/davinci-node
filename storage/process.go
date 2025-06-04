package storage

import (
	"encoding/json"
	"fmt"

	"github.com/vocdoni/vocdoni-z-sandbox/crypto/signatures/ethereum"
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

// ListProcesses returns the list of process IDs stored in the storage (by
// SetProcessMetadata) as a list of byte slices.
func (s *Storage) ListProcesses() ([][]byte, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	pids, err := s.listArtifacts(processPrefix)
	if err != nil {
		return nil, err
	}
	return pids, nil
}

// ListEndedProcesses returns the list of process IDs that are ended and have
// their encryption keys stored in the storage.
func (s *Storage) ListEndedProcesses() ([][]byte, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	pids, err := s.listArtifacts(encryptionKeyPrefix)
	if err != nil {
		return nil, err
	}

	// Filter out processes that are not ended or the encryption keys are not
	// in the storage.
	var finalPids [][]byte
	for _, pid := range pids {
		p := new(types.Process)
		if err := s.getArtifact(processPrefix, pid, p); err != nil {
			if err == ErrNotFound {
				continue // Skip if process not found
			}
			return nil, fmt.Errorf("error retrieving process %x: %w", pid, err)
		}
		if p.Status != types.ProcessStatusEnded {
			continue // Skip if process is not ended
		}
		finalPids = append(finalPids, pid)
	}
	return finalPids, nil
}

// MetadataHash returns the hash of the metadata.
func MetadataHash(metadata *types.Metadata) []byte {
	data, err := json.Marshal(metadata)
	if err != nil {
		panic(err)
	}
	return ethereum.HashRaw(data)
}
