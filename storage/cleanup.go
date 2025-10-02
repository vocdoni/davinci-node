package storage

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/vocdoni/davinci-node/db/prefixeddb"
	"github.com/vocdoni/davinci-node/log"
)

// CleanupEndedProcess removes all ballots, batches, state transitions and their
// reservations for a given processID. This method is called when a process is
// ended to free storage space. All votes that were not yet settled are marked
// as timeout before removal to preserve vote status for voter queries.
//
// The cleanup process handles:
// 1. Pending ballots (requires full iteration since they're keyed by voteID only)
// 2. Verified ballots (efficient iteration using processID prefix)
// 3. Aggregator batches (with vote ID status checking)
// 4. State transitions (preserving already settled votes)
// 5. Verified results
// 6. Vote ID statuses (marked as timeout, not deleted)
//
// Important: This method does NOT clean process metadata, encryption keys,
// or statistics as they serve as historical records.
func (s *Storage) CleanupEndedProcess(processID []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	var errs []error

	// Clean pending ballots
	if err := s.cleanPendingBallotsForProcess(processID); err != nil {
		errs = append(errs, fmt.Errorf("pending ballots: %w", err))
	}

	// Clean verified ballots
	if err := s.cleanVerifiedBallotsForProcess(processID); err != nil {
		errs = append(errs, fmt.Errorf("verified ballots: %w", err))
	}

	// Clean aggregator batches
	if err := s.cleanAggregatorBatchesForProcess(processID); err != nil {
		errs = append(errs, fmt.Errorf("aggregator batches: %w", err))
	}

	// Clean state transitions
	if err := s.cleanStateTransitionsForProcess(processID); err != nil {
		errs = append(errs, fmt.Errorf("state transitions: %w", err))
	}

	// Mark unsettled vote IDs as timeout (preserve vote status records for voters)
	if count, err := s.markProcessVoteIDsTimeout(processID); err != nil {
		errs = append(errs, fmt.Errorf("vote ID timeout marking: %w", err))
	} else {
		log.Debugw("marked vote IDs as timeout", "processID", fmt.Sprintf("%x", processID), "count", count)
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %v", errs)
	}

	log.Debugw("completed cleanup for ended process", "processID", fmt.Sprintf("%x", processID))
	return nil
}

// cleanPendingBallotsForProcess removes all pending ballots for a given processID.
// Since pending ballots are keyed by voteID only, we must iterate through all
// pending ballots and check their ProcessID field.
func (s *Storage) cleanPendingBallotsForProcess(processID []byte) error {
	pr := prefixeddb.NewPrefixedReader(s.db, ballotPrefix)
	var keysToDelete [][]byte

	// Collect all ballots that belong to this process
	if err := pr.Iterate(nil, func(k, v []byte) bool {
		var ballot Ballot
		if err := DecodeArtifact(v, &ballot); err != nil {
			log.Warnw("failed to decode pending ballot during cleanup", "key", hex.EncodeToString(k), "error", err)
			return true
		}

		if bytes.Equal(ballot.ProcessID, processID) {
			// Make copies to avoid slice reuse issues
			keyCopy := make([]byte, len(k))
			copy(keyCopy, k)
			keysToDelete = append(keysToDelete, keyCopy)
		}
		return true
	}); err != nil {
		return fmt.Errorf("iterate pending ballots: %w", err)
	}

	// Delete ballots and their reservations
	for _, key := range keysToDelete {
		// Delete reservation if exists
		if err := s.deleteArtifact(ballotReservationPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
			log.Warnw("failed to delete pending ballot reservation", "key", hex.EncodeToString(key), "error", err)
		}

		// Delete ballot
		if err := s.deleteArtifact(ballotPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
			log.Warnw("failed to delete pending ballot", "key", hex.EncodeToString(key), "error", err)
		}
	}

	log.Debugw("cleaned pending ballots", "processID", fmt.Sprintf("%x", processID), "count", len(keysToDelete))
	return nil
}

// cleanVerifiedBallotsForProcess removes all verified ballots for a given processID.
// Verified ballots are efficiently accessible using processID prefix.
func (s *Storage) cleanVerifiedBallotsForProcess(processID []byte) error {
	rd := prefixeddb.NewPrefixedReader(s.db, verifiedBallotPrefix)
	var keysToDelete [][]byte

	// Iterate with processID prefix for efficiency
	if err := rd.Iterate(processID, func(k, v []byte) bool {
		// Ensure key has processID prefix
		if len(k) < len(processID) || !bytes.Equal(k[:len(processID)], processID) {
			k = append(processID, k...)
		}

		// Always delete the ballot
		keyCopy := make([]byte, len(k))
		copy(keyCopy, k)
		keysToDelete = append(keysToDelete, keyCopy)
		return true
	}); err != nil {
		return fmt.Errorf("iterate verified ballots: %w", err)
	}

	// Delete ballots and their reservations
	for _, key := range keysToDelete {
		// Delete reservation if exists
		if err := s.deleteArtifact(verifiedBallotReservPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
			log.Warnw("failed to delete verified ballot reservation", "key", hex.EncodeToString(key), "error", err)
		}

		// Delete ballot
		if err := s.deleteArtifact(verifiedBallotPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
			log.Warnw("failed to delete verified ballot", "key", hex.EncodeToString(key), "error", err)
		}
	}

	log.Debugw("cleaned verified ballots", "processID", fmt.Sprintf("%x", processID), "count", len(keysToDelete))
	return nil
}

// cleanAggregatorBatchesForProcess removes all aggregator batches for a given processID.
func (s *Storage) cleanAggregatorBatchesForProcess(processID []byte) error {
	pr := prefixeddb.NewPrefixedReader(s.db, aggregBatchPrefix)
	var keysToDelete [][]byte

	// Iterate with processID prefix
	if err := pr.Iterate(processID, func(k, v []byte) bool {
		// Ensure key has processID prefix
		if len(k) < len(processID) || !bytes.Equal(k[:len(processID)], processID) {
			k = append(processID, k...)
		}

		keyCopy := make([]byte, len(k))
		copy(keyCopy, k)
		keysToDelete = append(keysToDelete, keyCopy)
		return true
	}); err != nil {
		return fmt.Errorf("iterate aggregator batches: %w", err)
	}

	// Delete batches and their reservations
	for _, key := range keysToDelete {
		// Delete reservation if exists
		if err := s.deleteArtifact(aggregBatchReservPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
			log.Warnw("failed to delete aggregator batch reservation", "key", hex.EncodeToString(key), "error", err)
		}

		// Delete batch
		if err := s.deleteArtifact(aggregBatchPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
			log.Warnw("failed to delete aggregator batch", "key", hex.EncodeToString(key), "error", err)
		}
	}

	log.Debugw("cleaned aggregator batches", "processID", fmt.Sprintf("%x", processID), "count", len(keysToDelete))
	return nil
}

// cleanStateTransitionsForProcess removes all state transitions for a given processID.
func (s *Storage) cleanStateTransitionsForProcess(processID []byte) error {
	pr := prefixeddb.NewPrefixedReader(s.db, stateTransitionPrefix)
	var keysToDelete [][]byte

	// Iterate with processID prefix
	if err := pr.Iterate(processID, func(k, v []byte) bool {
		// Ensure key has processID prefix
		if len(k) < len(processID) || !bytes.Equal(k[:len(processID)], processID) {
			k = append(processID, k...)
		}

		keyCopy := make([]byte, len(k))
		copy(keyCopy, k)
		keysToDelete = append(keysToDelete, keyCopy)
		return true
	}); err != nil {
		return fmt.Errorf("iterate state transitions: %w", err)
	}

	// Delete state transitions and their reservations
	for _, key := range keysToDelete {
		// Delete reservation if exists
		if err := s.deleteArtifact(stateTransitionReservPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
			log.Warnw("failed to delete state transition reservation", "key", hex.EncodeToString(key), "error", err)
		}

		// Delete state transition
		if err := s.deleteArtifact(stateTransitionPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
			log.Warnw("failed to delete state transition", "key", hex.EncodeToString(key), "error", err)
		}
	}

	log.Debugw("cleaned state transitions", "processID", fmt.Sprintf("%x", processID), "count", len(keysToDelete))
	return nil
}

// cleanAllReservations iterates over the given reservation prefix and removes
// all reservation entries. This ensures that no item remains "reserved" after
// a crash.
func (s *Storage) cleanAllReservations(prefix []byte) error {
	wTx := prefixeddb.NewPrefixedDatabase(s.db, prefix).WriteTx()
	defer wTx.Discard()
	var keysToDelete [][]byte
	// Collect all keys to delete
	if err := wTx.Iterate(nil, func(k, _ []byte) bool {
		kCopy := make([]byte, len(k))
		copy(kCopy, k)
		keysToDelete = append(keysToDelete, kCopy)
		return true
	}); err != nil {
		return fmt.Errorf("failed to iterate over reservation keys: %w", err)
	}
	// Delete them in a write transaction
	if len(keysToDelete) > 0 {
		log.Debugw("clearing queue reservations", "count", len(keysToDelete))
		for _, kk := range keysToDelete {
			if err := wTx.Delete(kk); err != nil {
				return fmt.Errorf("failed to delete reservation key %x: %w", kk, err)
			}
		}
		if err := wTx.Commit(); err != nil {
			return fmt.Errorf("failed to commit reservation deletion: %w", err)
		}
	}
	return nil
}
