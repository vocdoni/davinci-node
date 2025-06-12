package storage

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"

	"github.com/vocdoni/davinci-node/log"
	"go.vocdoni.io/dvote/db/prefixeddb"
)

// Vote ID status constants
const (
	VoteIDStatusPending = iota
	VoteIDStatusVerified
	VoteIDStatusAggregated
	VoteIDStatusProcessed
	VoteIDStatusSettled
	VoteIDStatusError
)

// voteIDStatusNames maps status codes to human-readable names
var voteIDStatusNames = map[int]string{
	VoteIDStatusPending:    "pending",
	VoteIDStatusVerified:   "verified",
	VoteIDStatusAggregated: "aggregated",
	VoteIDStatusProcessed:  "processed",
	VoteIDStatusSettled:    "settled",
	VoteIDStatusError:      "error",
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
	return s.markVoteIDsSettledUnsafe(processID, voteIDs)
}

// markVoteIDsSettledUnsafe marks a list of vote IDs as settled without acquiring locks.
// This method assumes the caller already holds the globalLock.
func (s *Storage) markVoteIDsSettledUnsafe(processID []byte, voteIDs [][]byte) error {
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

// CleanProcessVoteIDs removes all vote ID status entries for a given processID.
// Returns the number of entries removed and any error encountered.
func (s *Storage) CleanProcessVoteIDs(processID []byte) (int, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	log.Debugw("starting vote ID status cleanup", "processID", fmt.Sprintf("%x", processID))

	prefixedDB := prefixeddb.NewPrefixedDatabase(s.db, voteIDStatusPrefix)

	// read all keys and store them in memory
	var keysToDelete [][]byte
	if err := prefixedDB.Iterate(processID, func(k, _ []byte) bool {
		// Make a complete copy of the key to avoid any issues with the iteration
		keyCopy := make([]byte, len(k))
		copy(keyCopy, k)
		keysToDelete = append(keysToDelete, append(processID, keyCopy...))
		return true
	}); err != nil {
		return 0, fmt.Errorf("error iterating vote ID status keys: %w", err)
	}

	if len(keysToDelete) == 0 {
		return 0, nil
	}

	wTx := prefixedDB.WriteTx()
	defer wTx.Discard()

	// delete all keys in the transaction
	for _, key := range keysToDelete {
		if err := wTx.Delete(key); err != nil {
			log.Warnw("error deleting vote ID status", "key", fmt.Sprintf("%x", key), "error", err)
			return 0, fmt.Errorf("error deleting vote ID status: %w", err)
		}
	}

	if err := wTx.Commit(); err != nil {
		return 0, fmt.Errorf("error committing deletion transaction: %w", err)
	}

	return len(keysToDelete), nil
}

// setVoteIDStatus is an internal helper to set the status of a vote ID.
func (s *Storage) setVoteIDStatus(processID, voteID []byte, status int) error {
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), voteIDStatusPrefix)
	defer wTx.Discard()

	key := createVoteIDStatusKey(processID, voteID)
	statusBytes := intToBytes(status)

	if err := wTx.Set(key, statusBytes); err != nil {
		return fmt.Errorf("set vote ID status: %w", err)
	}

	return wTx.Commit()
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
