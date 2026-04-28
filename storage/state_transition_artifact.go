package storage

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

// PushStateTransitionArtifact stores a transition artifact keyed by the
// resulting root. The artifact is the durable record used by state sync and
// root promotion.
func (s *Storage) PushStateTransitionArtifact(artifact *state.TransitionArtifact) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	return s.pushStateTransitionArtifact(artifact)
}

// pushStateTransitionArtifact stores a transition artifact without acquiring
// the global lock. It assumes the caller already holds the lock.
func (s *Storage) pushStateTransitionArtifact(artifact *state.TransitionArtifact) error {
	if artifact == nil {
		return fmt.Errorf("nil state transition artifact")
	}
	if artifact.RootHashAfter == nil {
		return fmt.Errorf("state transition artifact has no rootAfter")
	}
	if artifact.ProcessID == (types.ProcessID{}) {
		return fmt.Errorf("state transition artifact has no processID")
	}
	key := stateTransitionArtifactKey(artifact.ProcessID, artifact.RootHashAfter)
	return s.setArtifact(stateTransitionArtifactPrefix, key, artifact)
}

// GetStateTransitionArtifact retrieves a transition artifact by processID and
// its resulting root.
func (s *Storage) GetStateTransitionArtifact(processID types.ProcessID, rootAfter *big.Int) (*state.TransitionArtifact, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	return s.getStateTransitionArtifact(processID, rootAfter)
}

// getStateTransitionArtifact retrieves a transition artifact by processID and
// its resulting root without acquiring the global lock.
func (s *Storage) getStateTransitionArtifact(processID types.ProcessID, rootAfter *big.Int) (*state.TransitionArtifact, error) {
	if rootAfter == nil {
		return nil, fmt.Errorf("nil rootAfter")
	}
	artifact := &state.TransitionArtifact{}
	key := stateTransitionArtifactKey(processID, rootAfter)
	if err := s.getArtifact(stateTransitionArtifactPrefix, key, artifact); err != nil {
		return nil, err
	}
	return artifact, nil
}

// removeStateTransitionArtifact removes a transition artifact by processID and
// rootAfter.
func (s *Storage) removeStateTransitionArtifact(processID types.ProcessID, rootAfter *big.Int) error {
	if rootAfter == nil {
		return fmt.Errorf("nil rootAfter")
	}
	key := stateTransitionArtifactKey(processID, rootAfter)
	return s.deleteArtifact(stateTransitionArtifactPrefix, key)
}

func stateTransitionArtifactKey(processID types.ProcessID, rootAfter *big.Int) []byte {
	key := bytes.Clone(processID.Bytes())
	if rootAfter != nil {
		key = append(key, rootAfter.Bytes()...)
	}
	return key
}
