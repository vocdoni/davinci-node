package storage

type PendingTxType []byte

var (
	// StateTransitionTx indicates a pending state transition transaction
	StateTransitionTx PendingTxType = []byte("st/")
)

// pendingTxs method retrieves the list of pending transaction hashes for a
// given process ID. If no pending transactions are found, it returns an empty
// list. If an error occurs during retrieval, it returns the error. This
// method is used internally to manage and track pending transactions
// associated and should be called with appropriate locking to ensure thread
// safety.
func (s *Storage) pendingTxs(txType PendingTxType, processID []byte) (bool, error) {
	// Get current pending txs
	var pendingTx bool
	prefix := append(pendingTxPrefix, txType...)
	if err := s.getArtifact(prefix, processID, &pendingTx); err != nil {
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
func (s *Storage) SetPendingTx(txType PendingTxType, processID []byte) error {
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
	return s.setArtifact(prefix, processID, true)
}

// HasPendingTx checks if a process has a pending state transition transaction.
func (s *Storage) HasPendingTx(txType PendingTxType, processID []byte) bool {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	pending, err := s.pendingTxs(txType, processID)
	return err == nil && pending
}

// ClearPendingTx removes the pending transaction marker for a process.
func (s *Storage) ClearPendingTx(txType PendingTxType, processID []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	prefix := append(pendingTxPrefix, txType...)
	return s.deleteArtifact(prefix, processID)
}
