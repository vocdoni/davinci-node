package storage

import (
	"fmt"
	"time"

	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

// Process retrieves the process data from the storage.
// It returns nil data and ErrNotFound if the metadata is not found.
func (s *Storage) Process(pid types.ProcessID) (*types.Process, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	return s.process(pid)
}

// process retrieves the process data from the storage without acquiring
// the globalLock. It assumes the caller already holds the lock.
func (s *Storage) process(pid types.ProcessID) (*types.Process, error) {
	p := &types.Process{}
	if err := s.getArtifact(processPrefix, pid.Bytes(), p); err != nil {
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
	if err := s.getArtifact(processPrefix, process.ID.Bytes(), existing); err == nil {
		return fmt.Errorf("process already exists: %x", process.ID)
	} else if err != ErrNotFound {
		return fmt.Errorf("failed to check process existence: %w", err)
	}

	// Create the process state
	pState, err := state.New(s.StateDB(), *process.ID)
	if err != nil {
		return fmt.Errorf("failed to create process state: %w", err)
	}

	// If process already has an EncryptionKey, store it
	if process.EncryptionKey != nil {
		if err := s.setEncryptionPubKeyUnsafe(*process.ID, process.EncryptionKey); err != nil {
			log.Warnw("failed to store encryption keys for process",
				"pid", process.ID.String(), "err", err.Error())
		}
	} else { // otherwise fetch or generate encryption keys for the process
		publicKey, _, err := s.fetchOrGenerateEncryptionKeysUnsafe(*process.ID)
		if err != nil {
			log.Warnw("failed to fetch or generate encryption keys for process",
				"pid", process.ID.String(), "err", err.Error())
		}
		ek := types.EncryptionKeyFromPoint(publicKey)
		process.EncryptionKey = &ek
	}

	// Initialize the process state to store the process data
	if err := pState.Initialize(
		process.Census.CensusOrigin.BigInt().MathBigInt(),
		circuits.BallotModeToCircuit(process.BallotMode),
		circuits.EncryptionKeyToCircuit(*process.EncryptionKey),
	); err != nil {
		return fmt.Errorf("failed to initialize process state: %w", err)
	}
	// Get the initial state root as big int to store in the process
	stateRoot, err := pState.RootAsBigInt()
	if err != nil {
		return fmt.Errorf("failed to get process state root: %w", err)
	}
	process.StateRoot = new(types.BigInt).SetBigInt(stateRoot)

	return s.setArtifact(processPrefix, process.ID.Bytes(), process)
}

// UpdateProcess performs an atomic read-modify-write operation on a process.
// The updateFunc is called with the current process state and can modify it.
// This ensures no race conditions between concurrent process updates.
func (s *Storage) UpdateProcess(processID types.ProcessID, updateFunc ...func(*types.Process) error) error {
	if !processID.IsValid() {
		return fmt.Errorf("invalid process ID")
	}
	if len(updateFunc) == 0 {
		return fmt.Errorf("no update function provided")
	}
	pidBytes := processID.Bytes()

	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Read current state
	p := &types.Process{}
	if err := s.getArtifact(processPrefix, pidBytes, p); err != nil {
		return fmt.Errorf("failed to get process for update: %w", err)
	}

	// Apply the update functions, each of which can modify the process state
	for _, f := range updateFunc {
		if err := f(p); err != nil {
			return fmt.Errorf("update function failed: %w", err)
		}
	}

	// Write back atomically
	if err := s.setArtifact(processPrefix, pidBytes, p); err != nil {
		return fmt.Errorf("failed to save updated process: %w", err)
	}

	return nil
}

// ListProcesses returns the list of process IDs stored in the storage (by
// SetProcessMetadata) as a list of byte slices.
func (s *Storage) ListProcesses() ([]types.ProcessID, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	pids, err := s.listArtifacts(processPrefix)
	if err != nil {
		return nil, err
	}
	processIDs := make([]types.ProcessID, len(pids))
	for i, b := range pids {
		pid, err := types.ProcessIDFromBytes(b)
		if err != nil {
			return nil, err
		}
		processIDs[i] = pid
	}
	return processIDs, nil
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
func (s *Storage) ProcessIsAcceptingVotes(pid types.ProcessID) (bool, error) {
	// Get the process from storage
	stgProcess, err := s.Process(pid)
	if err != nil {
		return false, fmt.Errorf("failed to get process %s: %w", pid.String(), err)
	}
	return s.processIsAcceptingVotes(pid, stgProcess)
}

// processIsAcceptingVotes checks if the process is ready to accept votes
// without acquiring the globalLock. It assumes the caller already holds the lock.
func (s *Storage) processIsAcceptingVotes(pid types.ProcessID, stgProcess *types.Process) (bool, error) {
	// Check that the process has a state root
	if stgProcess.StateRoot == nil {
		return false, fmt.Errorf("process %s has no state root", pid.String())
	}
	// Check if process has expired
	if stgProcess.StartTime.Add(stgProcess.Duration).Before(time.Now()) {
		return false, fmt.Errorf("process %s has expired", pid.String())
	}
	// Check if process is in ready state
	if stgProcess.Status != types.ProcessStatusReady {
		return false, fmt.Errorf("process %s status: %s", pid.String(), stgProcess.Status)
	}
	return true, nil
}

// ProcessMaxVotersReached checks if the process has reached its maximum
// number of voters based on the process ID provided.
func (s *Storage) ProcessMaxVotersReached(pid types.ProcessID) (bool, error) {
	// Get the process from storage
	p, err := s.Process(pid)
	if err != nil {
		return false, fmt.Errorf("failed to get process %s: %w", pid.String(), err)
	}
	return s.processMaxVotersReached(p)
}

// processMaxVotersReached checks if the process has reached its maximum
// number of voters based on the process data provided. It is a helper function
// used internally by ProcessMaxVotersReached and other storage package methods.
func (s *Storage) processMaxVotersReached(p *types.Process) (bool, error) {
	maxVoters := p.MaxVoters.MathBigInt().Uint64()
	if maxVoters == 0 {
		return false, fmt.Errorf("process %s has no max voters set", p.ID.String())
	}
	// If VotersCount is nil, it means no voters have been counted yet.
	if p.VotersCount == nil {
		return false, nil
	}
	return p.VotersCount.MathBigInt().Uint64() >= maxVoters, nil
}

// ListProcessWithEncryptionKeys returns a list of process IDs that have
// encryption keys stored.
func (s *Storage) ListProcessWithEncryptionKeys() ([]types.ProcessID, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	return s.listProcessesWithEncryptionKeys()
}

// ListEndedProcessWithEncryptionKeys returns the list of process IDs that are
// ended and have their encryption keys stored in the storage.
func (s *Storage) ListEndedProcessWithEncryptionKeys() ([]types.ProcessID, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Filter out processes that have the encryption keys stored.
	pids, err := s.listProcessesWithEncryptionKeys()
	if err != nil {
		return nil, err
	}

	// Filter the processes to only include those that are ended.
	var endedPids []types.ProcessID
	for _, pid := range pids {
		p := new(types.Process)
		if err := s.getArtifact(processPrefix, pid.Bytes(), p); err != nil {
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
func (s *Storage) CleanProcessStaleVotes(pid types.ProcessID) error {
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
	return nil
}

// listProcessesWithEncryptionKeys retrieves all process IDs that have
// encryption keys stored. It is a wrapper around listArtifacts with the
// encryptionKeyPrefix.
func (s *Storage) listProcessesWithEncryptionKeys() ([]types.ProcessID, error) {
	pids, err := s.listArtifacts(encryptionKeyPrefix)
	if err != nil {
		return nil, err
	}
	processIDs := make([]types.ProcessID, len(pids))
	for i, b := range pids {
		pid, err := types.ProcessIDFromBytes(b)
		if err != nil {
			return nil, err
		}
		processIDs[i] = pid
	}
	return processIDs, nil
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
		p, err := s.Process(pid)
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
				log.Infow("process status updated to ended", "pid", pid.String())
				// Cleanup ended process data
				if err := s.cleanupEndedProcess(pid); err != nil {
					log.Errorw(err, "failed to cleanup ended process "+pid.String())
				}
			}
		}
	}
}
