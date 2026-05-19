package storage

import (
	"fmt"
	"time"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

// Process retrieves the process data from the storage.
// It returns nil data and ErrNotFound if the metadata is not found.
func (s *Storage) Process(processID types.ProcessID) (*types.Process, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	return s.process(processID)
}

// process retrieves the process data from the storage without acquiring
// the globalLock. It assumes the caller already holds the lock.
func (s *Storage) process(processID types.ProcessID) (*types.Process, error) {
	p := &types.Process{}
	if err := s.getArtifact(processPrefix, processID.Bytes(), p); err != nil {
		return nil, err
	}
	return p, nil
}

// ProcessExists checks if a process exists in the storage. It returns false if
// the process is not found. If an error occurs, it returns the error.
func (s *Storage) ProcessExists(processID types.ProcessID) (bool, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	return s.processExists(processID)
}

// processExists checks if a process exists in the storage without acquiring
// the globalLock. It assumes the caller already holds the lock.
func (s *Storage) processExists(processID types.ProcessID) (bool, error) {
	p := &types.Process{}
	if err := s.getArtifact(processPrefix, processID.Bytes(), p); err != nil {
		if err == ErrNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
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
	if err := s.getArtifact(processPrefix, process.ID.Bytes(), &types.Process{}); err == nil {
		return fmt.Errorf("process already exists: %x", process.ID)
	} else if err != ErrNotFound {
		return fmt.Errorf("failed to check process existence: %w", err)
	}

	// Ensure the process has encryption keys
	if process.EncryptionKey == nil {
		return fmt.Errorf("invalid process: no encryption key provided")
	}
	// Try to store the encryption keys
	if err := s.setEncryptionPubKeyUnsafe(ProcessEncryptionKeyToPoint(process.EncryptionKey)); err != nil {
		log.Warnw("failed to store encryption keys for process",
			"processID", process.ID.String(), "error", err.Error())
	}
	// Ensure the process has a census
	if process.Census == nil {
		return fmt.Errorf("invalid process: no census provided")
	}
	// Ensure the process has a state root
	if process.StateRoot == nil {
		return fmt.Errorf("invalid process: no initial state root provided")
	}
	// Initialize the state tree and persist the initial root
	pState, err := state.New(s.StateDB(), *process.ID)
	if err != nil {
		return fmt.Errorf("failed to create process state: %w", err)
	}
	packedBallotMode, err := process.BallotMode.Pack()
	if err != nil {
		return fmt.Errorf("failed to pack ballot mode: %w", err)
	}
	if err := pState.Initialize(
		process.Census.CensusOrigin.BigInt().MathBigInt(),
		packedBallotMode,
		*process.EncryptionKey,
	); err != nil {
		return fmt.Errorf("failed to initialize process state: %w", err)
	}
	// Verify the computed root matches the process claim
	computedRoot, err := pState.RootAsBigInt()
	if err != nil {
		return fmt.Errorf("failed to get process state root: %w", err)
	}
	if !process.StateRoot.Equal((*types.BigInt)(computedRoot)) {
		return fmt.Errorf("process state root mismatch: expected %s, got %s",
			process.StateRoot.String(), computedRoot.String())
	}
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

	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Read current state
	p := &types.Process{}
	if err := s.getArtifact(processPrefix, processID.Bytes(), p); err != nil {
		return fmt.Errorf("failed to get process for update: %w", err)
	}

	// Apply the update functions, each of which can modify the process state
	for _, f := range updateFunc {
		if err := f(p); err != nil {
			return fmt.Errorf("update function failed: %w", err)
		}
	}

	// Write back atomically
	if err := s.setArtifact(processPrefix, processID.Bytes(), p); err != nil {
		return fmt.Errorf("failed to save updated process: %w", err)
	}

	return nil
}

// ListProcesses returns the list of process IDs stored in the storage (by
// SetProcessMetadata) as a list of byte slices.
func (s *Storage) ListProcesses() ([]types.ProcessID, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	pidsBytes, err := s.listArtifacts(processPrefix)
	if err != nil {
		return nil, err
	}
	processIDs := make([]types.ProcessID, len(pidsBytes))
	for i, b := range pidsBytes {
		processID, err := types.BytesToProcessID(b)
		if err != nil {
			return nil, err
		}
		processIDs[i] = processID
	}
	return processIDs, nil
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
	processIDs, err := s.listProcessesWithEncryptionKeys()
	if err != nil {
		return nil, err
	}

	// Filter the processes to only include those that are ended.
	var endedPids []types.ProcessID
	for _, processID := range processIDs {
		p := new(types.Process)
		if err := s.getArtifact(processPrefix, processID.Bytes(), p); err != nil {
			if err == ErrNotFound {
				continue // Skip if process not found
			}
			return nil, fmt.Errorf("error retrieving process %x: %w", processID, err)
		}
		if p.Status != types.ProcessStatusEnded {
			continue // Skip if process is not ended
		}
		endedPids = append(endedPids, processID)
	}
	return endedPids, nil
}

// CleanProcessStaleVotes removes all votes and related data for a given
// process ID. It cleans up pending ballots, verified ballots, aggregated
// ballots, and pending state transition batches associated with the process.
func (s *Storage) CleanProcessStaleVotes(processID types.ProcessID) error {
	// remove pending ballots
	if err := s.RemovePendingBallotsByProcess(processID); err != nil {
		return fmt.Errorf("error removing pending ballots for process %x: %w", processID, err)
	}
	// remove verified ballots (ready for aggregation)
	if err := s.RemoveVerifiedBallotsByProcess(processID); err != nil {
		return fmt.Errorf("error removing verified ballots for process %x: %w", processID, err)
	}
	// remove aggregated ballots (ready for state transition)
	if err := s.RemoveAggregatorBatchesByProcess(processID); err != nil {
		return fmt.Errorf("error removing ballot batches for process %x: %w", processID, err)
	}
	// remove pending state transitions batches
	if err := s.RemoveStateTransitionBatchesByProcess(processID); err != nil {
		return fmt.Errorf("error removing state transition batches for process %x: %w", processID, err)
	}
	return nil
}

// listProcessesWithEncryptionKeys retrieves all process IDs that have
// encryption keys available in storage.
func (s *Storage) listProcessesWithEncryptionKeys() ([]types.ProcessID, error) {
	pidsBytes, err := s.listArtifacts(processPrefix)
	if err != nil {
		return nil, err
	}
	processIDs := make([]types.ProcessID, 0, len(pidsBytes))
	for _, b := range pidsBytes {
		processID, err := types.BytesToProcessID(b)
		if err != nil {
			return nil, err
		}

		process := new(types.Process)
		if err := s.getArtifact(processPrefix, processID.Bytes(), process); err != nil {
			if err == ErrNotFound {
				continue
			}
			return nil, fmt.Errorf("error retrieving process %x: %w", processID, err)
		}
		if process.EncryptionKey == nil {
			continue
		}
		if _, _, err := s.encryptionKeysUnsafe(ProcessEncryptionKeyToPoint(process.EncryptionKey)); err != nil {
			continue
		}

		processIDs = append(processIDs, processID)
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
	processIDs, err := s.ListProcesses()
	if err != nil {
		log.Errorw(err, "failed to list  processes")
		return
	}

	for _, processID := range processIDs {
		p, err := s.Process(processID)
		if err != nil {
			log.Errorw(err, "failed to retrieve process for monitoring")
			continue
		}

		if p.Status != types.ProcessStatusEnded &&
			p.Status != types.ProcessStatusResults &&
			p.Status != types.ProcessStatusCanceled {
			if p.StartTime.Add(p.Duration).Before(time.Now()) {
				// Update the process status to ended
				if err := s.UpdateProcess(processID, func(p *types.Process) error {
					p.Status = types.ProcessStatusEnded
					return nil
				}); err != nil {
					log.Errorw(err, "failed to update process status to ended")
					continue
				}
				log.Infow("process status updated to ended", "processID", processID.String())
				// Cleanup ended process data
				if err := s.cleanupEndedProcess(processID); err != nil {
					log.Errorw(err, "failed to cleanup ended process "+processID.String())
				}
			}
		}
	}
}
