package storage

import "github.com/vocdoni/davinci-node/types"

type PendingTxType []byte

// StateTransitionTx indicates a pending state transition transaction
var StateTransitionTx PendingTxType = []byte("st/")

// pendingTxs method retrieves the list of pending transaction hashes for a
// given process ID. If no pending transactions are found, it returns an empty
// list. If an error occurs during retrieval, it returns the error. This
// method is used internally to manage and track pending transactions
// associated and should be called with appropriate locking to ensure thread
// safety.
func (s *Storage) pendingTxs(txType PendingTxType, processID types.ProcessID) (bool, error) {
	// Get current pending txs
	var pendingTx bool
	prefix := append(pendingTxPrefix, txType...)
	if err := s.getArtifact(prefix, processID.Bytes(), &pendingTx); err != nil {
		if err != ErrNotFound {
			return false, err
		}
		// If not found, start with empty list
		return false, nil
	}
	return pendingTx, nil
}

// SetPendingTx marks a process as having a pending on-chain
// transaction. If the process already has a pending transaction, it does
// nothing. If an error occurs during the operation, it returns the error.
func (s *Storage) SetPendingTx(txType PendingTxType, processID types.ProcessID) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Get if there are already pending tx for this process
	if currentTx, err := s.pendingTxs(txType, processID); err != nil {
		return err
	} else if currentTx {
		// The process already has a pending tx
		return nil
	}
	// Mark the process as having a pending tx
	prefix := append(pendingTxPrefix, txType...)
	return s.setArtifact(prefix, processID.Bytes(), true)
}

// HasPendingTx checks if a process has a pending state transition transaction.
func (s *Storage) HasPendingTx(txType PendingTxType, processID types.ProcessID) bool {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	pending, err := s.pendingTxs(txType, processID)
	return err == nil && pending
}

// PrunePendingTx removes the pending transaction marker for a process.
func (s *Storage) PrunePendingTx(txType PendingTxType, processID types.ProcessID) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	return s.prunePendingTx(txType, processID)
}

// prunePendingTx removes the pending transaction marker for a process. It
// should be called with appropriate locking to ensure thread safety.
func (s *Storage) prunePendingTx(txType PendingTxType, processID types.ProcessID) error {
	prefix := append(pendingTxPrefix, txType...)
	return s.deleteArtifact(prefix, processID.Bytes())
}
