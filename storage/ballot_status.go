package storage

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"

	"github.com/vocdoni/davinci-node/log"
	"go.vocdoni.io/dvote/db/prefixeddb"
)

// Ballot status constants
const (
	BallotStatusPending = iota
	BallotStatusVerified
	BallotStatusAggregated
	BallotStatusProcessed
	BallotStatusSettled
	BallotStatusError
)

// ballotStatusNames maps status codes to human-readable names
var ballotStatusNames = map[int]string{
	BallotStatusPending:    "pending",
	BallotStatusVerified:   "verified",
	BallotStatusAggregated: "aggregated",
	BallotStatusProcessed:  "processed",
	BallotStatusSettled:    "settled",
	BallotStatusError:      "error",
}

// BallotStatus returns the status of a ballot for a given processID and voteID.
// Returns ErrNotFound if the ballot status doesn't exist.
func (s *Storage) BallotStatus(processID, voteID []byte) (int, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	return s.ballotStatusUnsafe(processID, voteID)
}

// ballotStatusUnsafe returns the status of a ballot without acquiring locks.
// This method assumes the caller already holds the globalLock.
func (s *Storage) ballotStatusUnsafe(processID, voteID []byte) (int, error) {
	// Create the composite key: processID/voteID
	key := createBallotStatusKey(processID, voteID)

	// Get the status value
	reader := prefixeddb.NewPrefixedReader(s.db, ballotStatusPrefix)
	statusBytes, err := reader.Get(key)
	if err != nil || statusBytes == nil {
		return 0, ErrNotFound
	}

	// Convert bytes to int
	status, err := bytesToInt(statusBytes)
	if err != nil {
		return 0, fmt.Errorf("invalid ballot status format: %w", err)
	}

	return status, nil
}

// GetBallotStatusName returns the human-readable name of a ballot status.
func BallotStatusName(status int) string {
	if name, ok := ballotStatusNames[status]; ok {
		return name
	}
	return "unknown_status_" + strconv.Itoa(status)
}

// MarkBallotsSettled marks a list of ballots as settled for a given processID.
// This function is called after a state transition batch is confirmed on the blockchain.
func (s *Storage) MarkBallotsSettled(processID []byte, voteIDs [][]byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Use a transaction for better atomicity
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), ballotStatusPrefix)
	defer wTx.Discard()

	for _, voteID := range voteIDs {
		key := createBallotStatusKey(processID, voteID)
		status := intToBytes(BallotStatusSettled)

		if err := wTx.Set(key, status); err != nil {
			return fmt.Errorf("failed to mark ballot settled: %w", err)
		}
	}

	return wTx.Commit()
}

// CleanProcessBallots removes all ballot status entries for a given processID.
// Returns the number of entries removed and any error encountered.
func (s *Storage) CleanProcessBallots(processID []byte) (int, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	log.Debugw("starting ballot status cleanup", "processID", fmt.Sprintf("%x", processID))

	prefixedDB := prefixeddb.NewPrefixedDatabase(s.db, ballotStatusPrefix)

	// read all keys and store them in memory
	var keysToDelete [][]byte
	if err := prefixedDB.Iterate(processID, func(k, _ []byte) bool {
		// Make a complete copy of the key to avoid any issues with the iteration
		keyCopy := make([]byte, len(k))
		copy(keyCopy, k)
		keysToDelete = append(keysToDelete, append(processID, keyCopy...))
		return true
	}); err != nil {
		return 0, fmt.Errorf("error iterating ballot status keys: %w", err)
	}

	if len(keysToDelete) == 0 {
		return 0, nil
	}

	wTx := prefixedDB.WriteTx()
	defer wTx.Discard()

	// delete all keys in the transaction
	for _, key := range keysToDelete {
		if err := wTx.Delete(key); err != nil {
			log.Warnw("error deleting ballot status", "key", fmt.Sprintf("%x", key), "error", err)
			return 0, fmt.Errorf("error deleting ballot status: %w", err)
		}
	}

	if err := wTx.Commit(); err != nil {
		return 0, fmt.Errorf("error committing deletion transaction: %w", err)
	}

	return len(keysToDelete), nil
}

// setBallotStatus is an internal helper to set the status of a ballot.
func (s *Storage) setBallotStatus(processID, voteID []byte, status int) error {
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), ballotStatusPrefix)
	defer wTx.Discard()

	key := createBallotStatusKey(processID, voteID)
	statusBytes := intToBytes(status)

	if err := wTx.Set(key, statusBytes); err != nil {
		return fmt.Errorf("set ballot status: %w", err)
	}

	return wTx.Commit()
}

// Helper function to create a composite key for ballot status
func createBallotStatusKey(processID, voteID []byte) []byte {
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
