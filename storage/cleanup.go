package storage

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/vocdoni/davinci-node/db/prefixeddb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// cleanupEndedProcess removes all ballots, batches, state transitions and their
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
func (s *Storage) cleanupEndedProcess(processID types.ProcessID) error {
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
		log.Debugw("marked vote IDs as timeout", "processID", processID.String(), "count", count)
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %v", errs)
	}

	log.Debugw("completed cleanup for ended process", "processID", processID.String())
	return nil
}

// CleanAllPending removes all pending verified votes, all pending aggregated
// batches and all pending state transitions across all processes. All cleaned
// votes are marked with VoteIDStatusError status. This is a global cleanup
// operation that should be used with caution.
func (s *Storage) CleanAllPending() error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	var errs []error

	// Clean all verified ballots
	if err := s.cleanAllVerifiedBallots(); err != nil {
		errs = append(errs, fmt.Errorf("verified ballots: %w", err))
	}

	// Clean all aggregated batches
	if err := s.cleanAllAggregatedBatches(); err != nil {
		errs = append(errs, fmt.Errorf("aggregated batches: %w", err))
	}

	// Clean all state transitions
	if err := s.cleanAllStateTransitions(); err != nil {
		errs = append(errs, fmt.Errorf("state transitions: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %v", errs)
	}

	return nil
}

// cleanAllVerifiedBallots removes all verified ballots across all processes.
// This method assumes the caller already holds the globalLock.
func (s *Storage) cleanAllVerifiedBallots() error {
	rd := prefixeddb.NewPrefixedReader(s.db, verifiedBallotPrefix)

	// Track ballots by process for stats updates
	type processCleanup struct {
		keys       [][]byte
		validCount int
	}
	processBallots := make(map[types.ProcessID]*processCleanup)

	// Collect all verified ballots
	if err := rd.Iterate(nil, func(k, v []byte) bool {
		var ballot VerifiedBallot
		if err := DecodeArtifact(v, &ballot); err != nil {
			log.Warnw("failed to decode verified ballot during global cleanup",
				"key", hex.EncodeToString(k),
				"error", err)
			return true
		}

		if processBallots[ballot.ProcessID] == nil {
			processBallots[ballot.ProcessID] = &processCleanup{
				keys:       [][]byte{},
				validCount: 0,
			}
		}

		// Make a copy of the key
		keyCopy := make([]byte, len(k))
		copy(keyCopy, k)
		processBallots[ballot.ProcessID].keys = append(processBallots[ballot.ProcessID].keys, keyCopy)

		// Check current status to determine if we should count it for stats
		currentStatus, err := s.voteIDStatusUnsafe(ballot.ProcessID, ballot.VoteID)
		if err != nil {
			log.Warnw("could not get vote ID status during verified ballot cleanup",
				"processID", ballot.ProcessID.String(),
				"voteID", ballot.VoteID.String(),
				"error", err.Error())
			// Count it anyway as it might still be valid
			processBallots[ballot.ProcessID].validCount++
		} else if currentStatus == VoteIDStatusVerified {
			processBallots[ballot.ProcessID].validCount++
		} else {
			log.Warnw("vote ID is not in verified status during cleanup",
				"processID", ballot.ProcessID.String(),
				"voteID", ballot.VoteID.String(),
				"currentStatus", VoteIDStatusName(currentStatus))
		}

		// Mark vote ID as error only if not already settled
		if currentStatus != VoteIDStatusSettled {
			if err := s.setVoteIDStatus(ballot.ProcessID, ballot.VoteID, VoteIDStatusError); err != nil {
				log.Warnw("failed to set vote ID status to error",
					"processID", ballot.ProcessID.String(),
					"voteID", ballot.VoteID.String(),
					"error", err.Error())
			}
		}

		// Release vote ID lock
		s.releaseVoteID(ballot.VoteID)

		// Release address lock
		s.releaseAddress(ballot.ProcessID, ballot.Address)

		// Clean up voteID to address mapping
		s.voteIDToAddress.Delete(ballot.VoteID)

		return true
	}); err != nil {
		return fmt.Errorf("iterate verified ballots: %w", err)
	}

	// Delete all ballots and their reservations
	totalCleaned := 0
	for processID, cleanup := range processBallots {
		for _, key := range cleanup.keys {
			// Delete reservation if exists
			if err := s.deleteArtifact(verifiedBallotReservPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
				log.Warnw("failed to delete verified ballot reservation",
					"key", hex.EncodeToString(key),
					"error", err)
			}

			// Delete ballot
			if err := s.deleteArtifact(verifiedBallotPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
				log.Warnw("failed to delete verified ballot",
					"key", hex.EncodeToString(key),
					"error", err)
			}
		}

		totalCleaned += len(cleanup.keys)

		// Update process stats (only for ballots that were actually verified)
		if cleanup.validCount > 0 {
			if err := s.updateProcessStats(processID, []ProcessStatsUpdate{
				{TypeStats: types.TypeStatsVerifiedVotes, Delta: -cleanup.validCount},
				{TypeStats: types.TypeStatsCurrentBatchSize, Delta: -cleanup.validCount},
			}); err != nil {
				log.Warnw("failed to update process stats after cleaning verified ballots",
					"error", err.Error(),
					"processID", processID.String(),
					"validCount", cleanup.validCount)
			}
		}
	}
	if totalCleaned > 0 {
		log.Infow("cleaned all verified ballots", "count", totalCleaned)
	}
	return nil
}

// cleanAllAggregatedBatches removes all aggregated batches across all processes.
// This method assumes the caller already holds the globalLock.
func (s *Storage) cleanAllAggregatedBatches() error {
	pr := prefixeddb.NewPrefixedReader(s.db, aggregBatchPrefix)

	// Track batches by process for stats updates
	type processCleanup struct {
		keys       [][]byte
		validCount int
	}
	processBatches := make(map[types.ProcessID]*processCleanup)

	// Collect all aggregated batches
	if err := pr.Iterate(nil, func(k, v []byte) bool {
		var batch AggregatorBallotBatch
		if err := DecodeArtifact(v, &batch); err != nil {
			log.Warnw("failed to decode aggregated batch during global cleanup",
				"key", hex.EncodeToString(k),
				"error", err)
			return true
		}

		if processBatches[batch.ProcessID] == nil {
			processBatches[batch.ProcessID] = &processCleanup{
				keys:       [][]byte{},
				validCount: 0,
			}
		}

		// Make a copy of the key
		keyCopy := make([]byte, len(k))
		copy(keyCopy, k)
		processBatches[batch.ProcessID].keys = append(processBatches[batch.ProcessID].keys, keyCopy)

		// Process each ballot in the batch
		for _, ballot := range batch.Ballots {
			// Check current status to determine if we should count it for stats
			currentStatus, err := s.voteIDStatusUnsafe(batch.ProcessID, ballot.VoteID)
			if err != nil {
				log.Warnw("could not get vote ID status during batch cleanup",
					"processID", batch.ProcessID.String(),
					"voteID", ballot.VoteID.String(),
					"error", err.Error())
				// Count it anyway as it might still be valid
				processBatches[batch.ProcessID].validCount++
			} else if currentStatus == VoteIDStatusAggregated {
				processBatches[batch.ProcessID].validCount++
			} else {
				log.Warnw("vote ID is not in aggregated status during cleanup",
					"processID", batch.ProcessID.String(),
					"voteID", ballot.VoteID.String(),
					"currentStatus", VoteIDStatusName(currentStatus))
			}

			// Mark vote ID as error only if not already settled
			if currentStatus != VoteIDStatusSettled {
				if err := s.setVoteIDStatus(batch.ProcessID, ballot.VoteID, VoteIDStatusError); err != nil {
					log.Warnw("failed to set vote ID status to error",
						"processID", batch.ProcessID.String(),
						"voteID", ballot.VoteID.String(),
						"error", err.Error())
				}
			}

			// Release nullifier lock
			s.releaseVoteID(ballot.VoteID)
		}

		return true
	}); err != nil {
		return fmt.Errorf("iterate aggregated batches: %w", err)
	}

	// Delete all batches and their reservations
	totalCleaned := 0
	for processID, cleanup := range processBatches {
		for _, key := range cleanup.keys {
			// Delete reservation if exists
			if err := s.deleteArtifact(aggregBatchReservPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
				log.Warnw("failed to delete aggregated batch reservation",
					"key", hex.EncodeToString(key),
					"error", err)
			}

			// Delete batch
			if err := s.deleteArtifact(aggregBatchPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
				log.Warnw("failed to delete aggregated batch",
					"key", hex.EncodeToString(key),
					"error", err)
			}
		}

		totalCleaned += len(cleanup.keys)

		// Update process stats (only for ballots that were actually aggregated)
		if cleanup.validCount > 0 {
			if err := s.updateProcessStats(processID, []ProcessStatsUpdate{
				{TypeStats: types.TypeStatsAggregatedVotes, Delta: -cleanup.validCount},
				{TypeStats: types.TypeStatsCurrentBatchSize, Delta: -cleanup.validCount},
			}); err != nil {
				log.Warnw("failed to update process stats after cleaning aggregated batches",
					"error", err.Error(),
					"processID", processID.String(),
					"validCount", cleanup.validCount)
			}
		}
	}

	if totalCleaned > 0 {
		log.Infow("cleaned all aggregated batches", "count", totalCleaned)
	}
	return nil
}

// cleanAllStateTransitions removes all state transitions across all processes.
// This method assumes the caller already holds the globalLock.
func (s *Storage) cleanAllStateTransitions() error {
	pr := prefixeddb.NewPrefixedReader(s.db, stateTransitionPrefix)

	// Track transitions by process for stats updates
	type processCleanup struct {
		keys            [][]byte
		validBatchCount int
	}
	processTransitions := make(map[types.ProcessID]*processCleanup)

	// Collect all state transitions
	if err := pr.Iterate(nil, func(k, v []byte) bool {
		var stb StateTransitionBatch
		if err := DecodeArtifact(v, &stb); err != nil {
			log.Warnw("failed to decode state transition during global cleanup",
				"key", hex.EncodeToString(k),
				"error", err)
			return true
		}

		if processTransitions[stb.ProcessID] == nil {
			processTransitions[stb.ProcessID] = &processCleanup{
				keys:            [][]byte{},
				validBatchCount: 0,
			}
		}

		// Make a copy of the key
		keyCopy := make([]byte, len(k))
		copy(keyCopy, k)
		processTransitions[stb.ProcessID].keys = append(processTransitions[stb.ProcessID].keys, keyCopy)

		// Check if this batch should be counted (check first ballot status)
		batchIsValid := false
		if len(stb.Ballots) > 0 {
			currentStatus, err := s.voteIDStatusUnsafe(stb.ProcessID, stb.Ballots[0].VoteID)
			if err != nil {
				log.Warnw("could not get vote ID status during state transition cleanup",
					"processID", stb.ProcessID.String(),
					"voteID", stb.Ballots[0].VoteID.String(),
					"error", err.Error())
				// Count it anyway as it might still be valid
				batchIsValid = true
			} else if currentStatus == VoteIDStatusProcessed {
				batchIsValid = true
			} else {
				log.Warnw("vote ID is not in processed status during cleanup",
					"processID", stb.ProcessID.String(),
					"voteID", stb.Ballots[0].VoteID.String(),
					"currentStatus", VoteIDStatusName(currentStatus))
			}
		}

		if batchIsValid {
			processTransitions[stb.ProcessID].validBatchCount++
		}

		// Process each ballot in the batch
		for _, ballot := range stb.Ballots {
			// Check current status - only mark as error if not PROCESSED or SETTLED
			currentStatus, err := s.voteIDStatusUnsafe(stb.ProcessID, ballot.VoteID)
			if err != nil {
				log.Warnw("could not get vote ID status during state transition cleanup",
					"processID", stb.ProcessID.String(),
					"voteID", ballot.VoteID.String(),
					"error", err.Error())
			}

			// Only mark as error if not in PROCESSED or SETTLED status
			// PROCESSED votes are valid and just waiting for settlement
			// SETTLED votes have already been finalized
			if currentStatus != VoteIDStatusProcessed && currentStatus != VoteIDStatusSettled {
				if err := s.setVoteIDStatus(stb.ProcessID, ballot.VoteID, VoteIDStatusError); err != nil {
					log.Warnw("failed to set vote ID status to error",
						"processID", stb.ProcessID.String(),
						"voteID", ballot.VoteID.String(),
						"error", err.Error())
				}
			}

			// Release nullifier lock
			s.releaseVoteID(ballot.VoteID)
		}

		return true
	}); err != nil {
		return fmt.Errorf("iterate state transitions: %w", err)
	}

	// Delete all state transitions and their reservations
	totalCleaned := 0
	for processID, cleanup := range processTransitions {
		for _, key := range cleanup.keys {
			// Delete reservation if exists
			if err := s.deleteArtifact(stateTransitionReservPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
				log.Warnw("failed to delete state transition reservation",
					"key", hex.EncodeToString(key),
					"error", err)
			}

			// Delete state transition
			if err := s.deleteArtifact(stateTransitionPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
				log.Warnw("failed to delete state transition",
					"key", hex.EncodeToString(key),
					"error", err)
			}
		}

		totalCleaned += len(cleanup.keys)

		// Update process stats (count batches, not individual votes)
		if cleanup.validBatchCount > 0 {
			if err := s.updateProcessStats(processID, []ProcessStatsUpdate{
				{TypeStats: types.TypeStatsStateTransitions, Delta: -cleanup.validBatchCount},
			}); err != nil {
				log.Warnw("failed to update process stats after cleaning state transitions",
					"error", err.Error(),
					"processID", processID.String(),
					"validBatchCount", cleanup.validBatchCount)
			}
		}
	}
	if totalCleaned > 0 {
		log.Infow("cleaned all state transitions", "count", totalCleaned)
	}
	return nil
}

// cleanPendingBallotsForProcess removes all pending ballots for a given processID.
// Since pending ballots are keyed by voteID only, we must iterate through all
// pending ballots and check their ProcessID field.
func (s *Storage) cleanPendingBallotsForProcess(processID types.ProcessID) error {
	pr := prefixeddb.NewPrefixedReader(s.db, ballotPrefix)
	var ballotsToDelete []*Ballot

	// Collect all ballots that belong to this process
	if err := pr.Iterate(nil, func(k, v []byte) bool {
		var ballot Ballot
		if err := DecodeArtifact(v, &ballot); err != nil {
			log.Warnw("failed to decode pending ballot during cleanup", "key", hex.EncodeToString(k), "error", err)
			return true
		}

		if ballot.ProcessID == processID {
			ballotsToDelete = append(ballotsToDelete, &ballot)
		}
		return true
	}); err != nil {
		return fmt.Errorf("iterate pending ballots: %w", err)
	}

	// Delete ballots, their reservations, and release locks
	for _, ballot := range ballotsToDelete {
		// Delete reservation if exists
		if err := s.deleteArtifact(ballotReservationPrefix, ballot.VoteID.Bytes()); err != nil && !errors.Is(err, ErrNotFound) {
			log.Warnw("failed to delete pending ballot reservation", "voteID", ballot.VoteID.String(), "error", err)
		}

		// Delete ballot
		if err := s.deleteArtifact(ballotPrefix, ballot.VoteID.Bytes()); err != nil && !errors.Is(err, ErrNotFound) {
			log.Warnw("failed to delete pending ballot", "voteID", ballot.VoteID.String(), "error", err)
		}

		// Release locks
		s.releaseVoteID(ballot.VoteID)
		s.releaseAddress(ballot.ProcessID, ballot.Address)
		s.voteIDToAddress.Delete(ballot.VoteID)
	}
	if len(ballotsToDelete) > 0 {
		log.Debugw("cleaned pending ballots", "processID", processID.String(), "count", len(ballotsToDelete))
	}
	return nil
}

// cleanVerifiedBallotsForProcess removes all verified ballots for a given processID.
// Verified ballots are efficiently accessible using processID prefix.
func (s *Storage) cleanVerifiedBallotsForProcess(processID types.ProcessID) error {
	rd := prefixeddb.NewPrefixedReader(s.db, verifiedBallotPrefix)

	type ballotToDelete struct {
		key    []byte
		ballot *VerifiedBallot
	}
	var ballotsToDelete []ballotToDelete

	// Iterate with processID prefix for efficiency
	if err := rd.Iterate(processID.Bytes(), func(k, v []byte) bool {
		// Ensure key has processID prefix
		if len(k) < len(processID) || !bytes.Equal(k[:len(processID)], processID.Bytes()) {
			k = append(processID.Bytes(), k...)
		}

		// Decode ballot to get address for lock release
		var ballot VerifiedBallot
		if err := DecodeArtifact(v, &ballot); err != nil {
			log.Warnw("failed to decode verified ballot during cleanup",
				"key", hex.EncodeToString(k),
				"error", err)
			// Still add to deletion list even if we can't decode
			keyCopy := make([]byte, len(k))
			copy(keyCopy, k)
			ballotsToDelete = append(ballotsToDelete, ballotToDelete{key: keyCopy, ballot: nil})
		} else {
			keyCopy := make([]byte, len(k))
			copy(keyCopy, k)
			ballotsToDelete = append(ballotsToDelete, ballotToDelete{key: keyCopy, ballot: &ballot})
		}
		return true
	}); err != nil {
		return fmt.Errorf("iterate verified ballots: %w", err)
	}

	// Delete ballots, their reservations, and release locks
	for _, item := range ballotsToDelete {
		// Delete reservation if exists
		if err := s.deleteArtifact(verifiedBallotReservPrefix, item.key); err != nil && !errors.Is(err, ErrNotFound) {
			log.Warnw("failed to delete verified ballot reservation", "key", hex.EncodeToString(item.key), "error", err)
		}

		// Delete ballot
		if err := s.deleteArtifact(verifiedBallotPrefix, item.key); err != nil && !errors.Is(err, ErrNotFound) {
			log.Warnw("failed to delete verified ballot", "key", hex.EncodeToString(item.key), "error", err)
		}

		// Release locks if we successfully decoded the ballot
		if item.ballot != nil {
			s.releaseVoteID(item.ballot.VoteID)
			s.releaseAddress(item.ballot.ProcessID, item.ballot.Address)
			s.voteIDToAddress.Delete(item.ballot.VoteID)
		}
	}

	if len(ballotsToDelete) > 0 {
		log.Debugw("cleaned verified ballots", "processID", processID.String(), "count", len(ballotsToDelete))
	}
	return nil
}

// cleanAggregatorBatchesForProcess removes all aggregator batches for a given processID.
func (s *Storage) cleanAggregatorBatchesForProcess(processID types.ProcessID) error {
	pr := prefixeddb.NewPrefixedReader(s.db, aggregBatchPrefix)
	var keysToDelete [][]byte

	// Iterate with processID prefix
	if err := pr.Iterate(processID.Bytes(), func(k, v []byte) bool {
		// Ensure key has processID prefix
		if len(k) < len(processID) || !bytes.Equal(k[:len(processID)], processID.Bytes()) {
			k = append(processID.Bytes(), k...)
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
	if len(keysToDelete) > 0 {
		log.Debugw("cleaned aggregator batches", "processID", processID.String(), "count", len(keysToDelete))
	}
	return nil
}

// cleanStateTransitionsForProcess removes all state transitions for a given processID.
func (s *Storage) cleanStateTransitionsForProcess(processID types.ProcessID) error {
	pr := prefixeddb.NewPrefixedReader(s.db, stateTransitionPrefix)
	var keysToDelete [][]byte

	// Iterate with processID prefix
	if err := pr.Iterate(processID.Bytes(), func(k, v []byte) bool {
		// Ensure key has processID prefix
		if len(k) < len(processID) || !bytes.Equal(k[:len(processID)], processID.Bytes()) {
			k = append(processID.Bytes(), k...)
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
	if len(keysToDelete) > 0 {
		log.Debugw("cleaned state transitions", "processID", processID.String(), "count", len(keysToDelete))
	}
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
