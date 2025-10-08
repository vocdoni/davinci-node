package storage

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"

	"github.com/vocdoni/davinci-node/db/prefixeddb"
	"github.com/vocdoni/davinci-node/log"
)

// Vote ID status constants
const (
	VoteIDStatusPending = iota
	VoteIDStatusVerified
	VoteIDStatusAggregated
	VoteIDStatusProcessed
	VoteIDStatusSettled
	VoteIDStatusError
	VoteIDStatusTimeout
)

// voteIDStatusNames maps status codes to human-readable names
var voteIDStatusNames = map[int]string{
	VoteIDStatusPending:    "pending",
	VoteIDStatusVerified:   "verified",
	VoteIDStatusAggregated: "aggregated",
	VoteIDStatusProcessed:  "processed",
	VoteIDStatusSettled:    "settled",
	VoteIDStatusError:      "error",
	VoteIDStatusTimeout:    "timeout",
}

// VoteIDStatus returns the status of a vote ID for a given processID and voteID.
// Returns ErrNotFound if the vote ID status doesn't exist.
func (s *Storage) VoteIDStatus(processID, voteID []byte) (int, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	return s.voteIDStatusUnsafe(processID, voteID)
}

// voteIDStatusUnsafe returns the status of a vote ID without acquiring locks.
// This method assumes the caller already holds the globalLock.
func (s *Storage) voteIDStatusUnsafe(processID, voteID []byte) (int, error) {
	// Create the composite key: processID/voteID
	key := createVoteIDStatusKey(processID, voteID)

	// Get the status value
	reader := prefixeddb.NewPrefixedReader(s.db, voteIDStatusPrefix)
	statusBytes, err := reader.Get(key)
	if err != nil || statusBytes == nil {
		return 0, ErrNotFound
	}

	// Convert bytes to int
	status, err := bytesToInt(statusBytes)
	if err != nil {
		return 0, fmt.Errorf("invalid vote ID status format: %w", err)
	}

	return status, nil
}

// VoteIDStatusName returns the human-readable name of a vote ID status.
func VoteIDStatusName(status int) string {
	if name, ok := voteIDStatusNames[status]; ok {
		return name
	}
	return "unknown_status_" + strconv.Itoa(status)
}

// MarkVoteIDsSettled marks a list of vote IDs as settled for a given processID.
// This function is called after a state transition batch is confirmed on the blockchain.
func (s *Storage) MarkVoteIDsSettled(processID []byte, voteIDs [][]byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	return s.markVoteIDsSettled(processID, voteIDs)
}

// markVoteIDsSettledUnsafe marks a list of vote IDs as settled without acquiring locks.
// This method assumes the caller already holds the globalLock.
func (s *Storage) markVoteIDsSettled(processID []byte, voteIDs [][]byte) error {
	// Use a transaction for better atomicity
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), voteIDStatusPrefix)
	defer wTx.Discard()

	for _, voteID := range voteIDs {
		key := createVoteIDStatusKey(processID, voteID)
		status := intToBytes(VoteIDStatusSettled)

		if err := wTx.Set(key, status); err != nil {
			return fmt.Errorf("failed to mark vote ID settled: %w", err)
		}
	}

	return wTx.Commit()
}

// MarkProcessVoteIDsTimeout marks all unsettled vote IDs for a process as timeout.
// This is called when a process ends to indicate that votes were not processed
// due to process termination, but preserves the vote ID records for voter queries.
func (s *Storage) MarkProcessVoteIDsTimeout(processID []byte) (int, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	return s.markProcessVoteIDsTimeout(processID)
}

// markProcessVoteIDsTimeoutUnsafe marks all unsettled vote IDs for a process as timeout
// without acquiring locks. This method assumes the caller already holds the globalLock.
func (s *Storage) markProcessVoteIDsTimeout(processID []byte) (int, error) {
	prefixedDB := prefixeddb.NewPrefixedDatabase(s.db, voteIDStatusPrefix)
	wTx := prefixedDB.WriteTx()
	defer wTx.Discard()

	var updatedCount int

	// Iterate through all vote IDs for this process
	if err := prefixedDB.Iterate(processID, func(k, v []byte) bool {
		// Create the full key
		fullKey := append(processID, k...)

		// Get current status
		currentStatus, err := bytesToInt(v)
		if err != nil {
			log.Warnw("invalid vote ID status format during timeout marking",
				"key", fmt.Sprintf("%x", fullKey), "error", err)
			return true
		}

		// Only mark as timeout if not already settled
		if currentStatus != VoteIDStatusSettled {
			timeoutStatus := intToBytes(VoteIDStatusTimeout)
			if err := wTx.Set(fullKey, timeoutStatus); err != nil {
				log.Warnw("failed to mark vote ID as timeout",
					"key", fmt.Sprintf("%x", fullKey), "error", err)
				return true
			}
			updatedCount++
		}
		return true
	}); err != nil {
		return 0, fmt.Errorf("error iterating vote ID status keys: %w", err)
	}

	if err := wTx.Commit(); err != nil {
		return 0, fmt.Errorf("error committing timeout status updates: %w", err)
	}

	log.Debugw("marked vote IDs as timeout", "processID", fmt.Sprintf("%x", processID), "count", updatedCount)
	return updatedCount, nil
}

// setVoteIDStatus is an internal helper to set the status of a vote ID.
// It enforces status transition rules to prevent invalid state changes:
// - SETTLED status is final and cannot be changed
// - Status transitions must follow the valid progression
func (s *Storage) setVoteIDStatus(processID, voteID []byte, status int) error {
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), voteIDStatusPrefix)
	defer wTx.Discard()

	key := createVoteIDStatusKey(processID, voteID)

	// Check current status to enforce transition rules
	currentStatusBytes, err := wTx.Get(key)
	if err == nil && currentStatusBytes != nil {
		currentStatus, err := bytesToInt(currentStatusBytes)
		if err == nil {
			// SETTLED is a final status - cannot be changed
			if currentStatus == VoteIDStatusSettled {
				log.Debugw("attempted to change settled vote status",
					"processID", fmt.Sprintf("%x", processID),
					"voteID", fmt.Sprintf("%x", voteID),
					"currentStatus", VoteIDStatusName(currentStatus),
					"attemptedStatus", VoteIDStatusName(status))
				return nil // Silently ignore - this is expected behavior
			}

			// Validate status transition
			if !isValidStatusTransition(currentStatus, status) {
				log.Warnw("invalid vote status transition",
					"processID", fmt.Sprintf("%x", processID),
					"voteID", fmt.Sprintf("%x", voteID),
					"from", VoteIDStatusName(currentStatus),
					"to", VoteIDStatusName(status))
				// Allow the transition but log the warning
				// This prevents breaking existing flows while alerting us to issues
			}
		}
	}

	statusBytes := intToBytes(status)
	if err := wTx.Set(key, statusBytes); err != nil {
		return fmt.Errorf("set vote ID status: %w", err)
	}

	return wTx.Commit()
}

// isValidStatusTransition checks if a status transition is valid.
// Valid transitions follow this flow:
// PENDING → VERIFIED → AGGREGATED → PROCESSED → SETTLED
// Any status can transition to ERROR or TIMEOUT (except SETTLED)
func isValidStatusTransition(from, to int) bool {
	// SETTLED is final - no transitions allowed
	if from == VoteIDStatusSettled {
		return false
	}

	// ERROR and TIMEOUT are terminal states (except from SETTLED)
	if to == VoteIDStatusError || to == VoteIDStatusTimeout {
		return true
	}

	// SETTLED can only be reached from PROCESSED
	if to == VoteIDStatusSettled {
		return from == VoteIDStatusProcessed
	}

	// Valid forward progressions
	validTransitions := map[int][]int{
		VoteIDStatusPending:    {VoteIDStatusVerified},
		VoteIDStatusVerified:   {VoteIDStatusAggregated},
		VoteIDStatusAggregated: {VoteIDStatusProcessed},
		VoteIDStatusProcessed:  {VoteIDStatusSettled},
	}

	allowedNext, exists := validTransitions[from]
	if !exists {
		return false
	}

	for _, allowed := range allowedNext {
		if to == allowed {
			return true
		}
	}

	return false
}

// Helper function to create a composite key for vote ID status
func createVoteIDStatusKey(processID, voteID []byte) []byte {
	return slices.Concat(processID, voteID)
}

// Helper function to convert int to bytes
func intToBytes(i int) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(i))
	return b
}

// Helper function to convert bytes to int
func bytesToInt(b []byte) (int, error) {
	if len(b) != 8 {
		return 0, fmt.Errorf("invalid byte length for int conversion: %d", len(b))
	}
	return int(binary.LittleEndian.Uint64(b)), nil
}
