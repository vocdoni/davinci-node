package storage

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
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

// NewProcess stores a new process and its metadata into the storage.
// It checks that the process doesn't already exist to prevent accidental overwrites.
// For updating existing processes, use UpdateProcess instead to avoid race conditions.
func (s *Storage) NewProcess(process *types.Process) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	if process == nil {
		return fmt.Errorf("nil process data")
	}

	// Check if process already exists
	existing := &types.Process{}
	if err := s.getArtifact(processPrefix, process.ID, existing); err == nil {
		return fmt.Errorf("process already exists: %x", process.ID)
	} else if err != ErrNotFound {
		return fmt.Errorf("failed to check process existence: %w", err)
	}

	// Create the process state
	pState, err := state.New(s.StateDB(), process.ID.BigInt().MathBigInt())
	if err != nil {
		return fmt.Errorf("failed to create process state: %w", err)
	}

	// Parse the process ID from the byte slice
	pid := new(types.ProcessID).SetBytes(process.ID)

	// Fetch or generate encryption keys for the process
	publicKey, _, err := s.fetchOrGenerateEncryptionKeysUnsafe(pid)
	if err != nil {
		log.Warnw("failed to fetch or generate encryption keys for process",
			"pid", pid.String(), "err", err.Error())
	}

	// censusRoot, err := process.BigCensusRoot()
	// if err != nil {
	// 	return fmt.Errorf("failed to get census root: %w", err)
	// }

	// Initialize the process state to store the process data
	if err := pState.Initialize(
		process.Census.CensusOrigin.BigInt().MathBigInt(),
		// censusRoot.MathBigInt(),
		circuits.BallotModeToCircuit(process.BallotMode),
		circuits.EncryptionKeyFromECCPoint(publicKey),
	); err != nil {
		return fmt.Errorf("failed to initialize process state: %w", err)
	}
	// Get the initial state root as big int to store in the process
	stateRoot, err := pState.RootAsBigInt()
	if err != nil {
		return fmt.Errorf("failed to get process state root: %w", err)
	}
	process.StateRoot = new(types.BigInt).SetBigInt(stateRoot)

	return s.setArtifact(processPrefix, process.ID, process)
}

// UpdateProcess performs an atomic read-modify-write operation on a process.
// The updateFunc is called with the current process state and can modify it.
// This ensures no race conditions between concurrent process updates.
func (s *Storage) UpdateProcess(pid []byte, updateFunc ...func(*types.Process) error) error {
	if pid == nil {
		return fmt.Errorf("nil process ID")
	}
	if len(updateFunc) == 0 {
		return fmt.Errorf("no update function provided")
	}

	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Read current state
	p := &types.Process{}
	if err := s.getArtifact(processPrefix, pid, p); err != nil {
		return fmt.Errorf("failed to get process for update: %w", err)
	}

	// Apply the update functions, each of which can modify the process state
	for _, f := range updateFunc {
		if err := f(p); err != nil {
			return fmt.Errorf("update function failed: %w", err)
		}
	}

	// Write back atomically
	if err := s.setArtifact(processPrefix, pid, p); err != nil {
		return fmt.Errorf("failed to save updated process: %w", err)
	}

	return nil
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

// SetMetadata stores the metadata into the storage.
func (s *Storage) SetMetadata(metadata *types.Metadata) ([]byte, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	if metadata == nil {
		return nil, fmt.Errorf("nil metadata")
	}

	// Calculate the hash of the metadata
	hash := MetadataHash(metadata)

	// Store the metadata with its hash as the key
	return hash, s.setArtifact(metadataPrefix, hash, metadata, ArtifactEncodingJSON)
}

// GetMetadata retrieves the metadata from the storage using its hash.
func (s *Storage) Metadata(hash []byte) (*types.Metadata, error) {
	if hash == nil {
		return nil, fmt.Errorf("nil metadata hash")
	}
	// Try to get the metadata from the cache
	val, ok := s.cache.Get(string(metadataPrefix) + string(hash))
	if ok {
		if metadata, ok := val.(*types.Metadata); ok {
			return metadata, nil
		}
		log.Warnw("cache hit but type assertion failed", "expected", "*types.Metadata", "got", fmt.Sprintf("%T", val))
	}

	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Retrieve the metadata from the storage
	metadata := &types.Metadata{}
	if err := s.getArtifact(metadataPrefix, hash, metadata, ArtifactEncodingJSON); err != nil {
		return nil, err
	}

	// Store the metadata in the cache for future use
	s.cache.Add(string(metadataPrefix)+string(hash), metadata)

	return metadata, nil
}

// ProcessIsAcceptingVotes checks if the process is ready to accept votes,
// which means that the process is in the "Ready" state.
func (s *Storage) ProcessIsAcceptingVotes(pid []byte) (bool, error) {
	// Get the process from storage
	processID := new(types.ProcessID).SetBytes(pid)
	stgProcess, err := s.Process(processID)
	if err != nil {
		return false, fmt.Errorf("failed to get process %x: %w", pid, err)
	}
	// Basic checks
	if stgProcess.StateRoot == nil {
		return false, fmt.Errorf("process %x has no state root", pid)
	}
	if stgProcess.StartTime.Add(stgProcess.Duration).Before(time.Now()) {
		return false, fmt.Errorf("process %x has expired", pid)
	}
	if stgProcess.Status != types.ProcessStatusReady {
		return false, fmt.Errorf("process %x status: %s", pid, stgProcess.Status)
	}
	return true, nil
}

// ListProcessWithEncryptionKeys returns a list of process IDs that have
// encryption keys stored.
func (s *Storage) ListProcessWithEncryptionKeys() ([][]byte, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	pids, err := s.listProcessesWithEncryptionKeys()
	if err != nil {
		return nil, err
	}
	return pids, nil
}

// ListEndedProcessWithEncryptionKeys returns the list of process IDs that are
// ended and have their encryption keys stored in the storage.
func (s *Storage) ListEndedProcessWithEncryptionKeys() ([][]byte, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Filter out processes that have the encryption keys stored.
	pids, err := s.listProcessesWithEncryptionKeys()
	if err != nil {
		return nil, err
	}

	// Filter the processes to only include those that are ended.
	var endedPids [][]byte
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
		endedPids = append(endedPids, pid)
	}
	return endedPids, nil
}

// CleanProcessStaleVotes removes all votes and related data for a given
// process ID. It cleans up pending ballots, verified ballots, aggregated
// ballots, and pending state transition batches associated with the process.
func (s *Storage) CleanProcessStaleVotes(pid []byte) error {
	// remove pending ballots
	if err := s.RemovePendingBallotsByProcess(pid); err != nil {
		return fmt.Errorf("error removing pending ballots for process %x: %w", pid, err)
	}
	// remove verified ballots (ready for aggregation)
	if err := s.RemoveVerifiedBallotsByProcess(pid); err != nil {
		return fmt.Errorf("error removing verified ballots for process %x: %w", pid, err)
	}
	// remove aggregated ballots (ready for state transition)
	if err := s.RemoveAggregatorBatchesByProcess(pid); err != nil {
		return fmt.Errorf("error removing ballot batches for process %x: %w", pid, err)
	}
	// remove pending state transitions batches
	if err := s.RemoveStateTransitionBatchesByProcess(pid); err != nil {
		return fmt.Errorf("error removing state transition batches for process %x: %w", pid, err)
	}
	log.Infow("cleaned up stale votes for finalized process", "pid", fmt.Sprintf("%x", pid))
	return nil
}

// listProcessesWithEncryptionKeys retrieves all process IDs that have
// encryption keys stored. It is a wrapper around listArtifacts with the
// encryptionKeyPrefix.
func (s *Storage) listProcessesWithEncryptionKeys() ([][]byte, error) {
	return s.listArtifacts(encryptionKeyPrefix)
}

// monitorEndedProcesses starts a goroutine that periodically checks for processes
// that have ended and updates their status accordingly.
// It runs every 30 seconds to ensure that processes that have reached their
// duration are marked as ended.
func (s *Storage) monitorEndedProcesses() {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				log.Info("monitorEndedProcesses stopped")
				return
			case <-ticker.C:
				s.checkAndUpdateEndedProcesses()
			}
		}
	}()
}

// checkAndUpdateEndedProcesses checks for processes that have ended based on their
// start time and duration.
func (s *Storage) checkAndUpdateEndedProcesses() {
	pids, err := s.ListProcesses()
	if err != nil {
		log.Errorw(err, "failed to list  processes")
		return
	}

	for _, pid := range pids {
		p, err := s.Process(new(types.ProcessID).SetBytes(pid))
		if err != nil {
			log.Errorw(err, "failed to retrieve process for monitoring")
			continue
		}

		if p.Status != types.ProcessStatusEnded &&
			p.Status != types.ProcessStatusResults &&
			p.Status != types.ProcessStatusCanceled {
			if p.StartTime.Add(p.Duration).Before(time.Now()) {
				// Update the process status to ended
				if err := s.UpdateProcess(pid, func(p *types.Process) error {
					p.Status = types.ProcessStatusEnded
					return nil
				}); err != nil {
					log.Errorw(err, "failed to update process status to ended")
					continue
				}
				log.Infow("process status updated to ended", "pid", hex.EncodeToString(pid))
				if err := s.cleanupEndedProcess(pid); err != nil {
					log.Errorw(err, "failed to cleanup ended process "+hex.EncodeToString(pid))
				}
			}
		}
	}
}
