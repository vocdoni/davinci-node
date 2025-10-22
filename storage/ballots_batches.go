package storage

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/prefixeddb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// PushAggregatorBatch pushes an aggregated ballot batch to the aggregator queue.
func (s *Storage) PushAggregatorBatch(abb *AggregatorBallotBatch) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	val, err := EncodeArtifact(abb)
	if err != nil {
		return fmt.Errorf("encode batch: %w", err)
	}
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), aggregBatchPrefix)
	key := hashKey(val)
	if err := wTx.Set(append(slices.Clone(abb.ProcessID), key...), val); err != nil {
		wTx.Discard()
		return err
	}
	if err := wTx.Commit(); err != nil {
		return err
	}

	// Update process stats
	if err := s.updateProcessStats(abb.ProcessID, []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsAggregatedVotes, Delta: len(abb.Ballots)},
		{TypeStats: types.TypeStatsLastBatchSize, Delta: len(abb.Ballots)},
		{TypeStats: types.TypeStatsCurrentBatchSize, Delta: -len(abb.Ballots)},
	}); err != nil {
		return fmt.Errorf("failed to update process stats: %w", err)
	}

	// Update status of all vote IDs in the batch to aggregated
	// TODO: this should use a single write transaction
	for _, ballot := range abb.Ballots {
		if err := s.setVoteIDStatus(abb.ProcessID, ballot.VoteID, VoteIDStatusAggregated); err != nil {
			log.Warnw("failed to set vote ID status to aggregated", "error", err.Error())
		}
	}

	return nil
}

// RemoveAggregatorBatchesByProcess removes all ballot batches for a given processID.
func (s *Storage) RemoveAggregatorBatchesByProcess(pid []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	// get every batch key for the process
	batchesToRemove := []types.HexBytes{}
	rd := prefixeddb.NewPrefixedReader(s.db, aggregBatchPrefix)
	if err := rd.Iterate(pid, func(k, _ []byte) bool {
		// Append the processID prefix to the key if missing (depends on the database implementation)
		if len(k) < len(pid) || !bytes.Equal(k[:len(pid)], pid) {
			k = append(pid, k...)
		}
		batchesToRemove = append(batchesToRemove, k)
		return true
	}); err != nil {
		return fmt.Errorf("iterate over ballot batches: %w", err)
	}
	// iterate over all keys to remove the reservation and the batch
	for _, k := range batchesToRemove {
		if err := s.deleteArtifact(aggregBatchReservPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("delete batch reservation: %w", err)
		}
		if err := s.deleteArtifact(aggregBatchPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("delete aggregator batch: %w", err)
		}
	}
	// TODO: check if we need to update stats here
	return nil
}

// MarkAggregatorBatchFailed marks a ballot batch as failed, sets all ballots
// in the batch to error status, removes the reservation, and deletes the batch
// from the aggregator queue. This is typically called when the batch processing
// fails or is not valid.
func (s *Storage) MarkAggregatorBatchFailed(key []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	pr := prefixeddb.NewPrefixedReader(s.db, aggregBatchPrefix)
	val, err := pr.Get(key)
	if err != nil {
		return fmt.Errorf("get batch: %w", err)
	}

	agg := new(AggregatorBallotBatch)
	if err := DecodeArtifact(val, agg); err != nil {
		return fmt.Errorf("decode batch: %w", err)
	}

	validAggregatedCount := 0

	// Mark all vote IDs in the batch as error and count how many were actually aggregated
	for _, ballot := range agg.Ballots {
		// Check current vote ID status to avoid double-processing
		currentStatus, err := s.voteIDStatusUnsafe(agg.ProcessID, ballot.VoteID)
		if err != nil {
			log.Warnw("could not get vote ID status during batch failure",
				"processID", fmt.Sprintf("%x", agg.ProcessID),
				"voteID", hex.EncodeToString(ballot.VoteID),
				"error", err.Error())
			// Continue processing as the ballot might still be valid
			validAggregatedCount++
		} else if currentStatus == VoteIDStatusAggregated {
			// Only count ballots that were actually in aggregated status
			validAggregatedCount++
		} else {
			log.Warnw("vote ID is not in aggregated status during batch failure",
				"processID", fmt.Sprintf("%x", agg.ProcessID),
				"voteID", hex.EncodeToString(ballot.VoteID),
				"currentStatus", VoteIDStatusName(currentStatus))
		}

		// Release nullifier lock
		s.releaseVoteID(ballot.VoteID.BigInt().MathBigInt())

		// Set vote ID status to error
		if err := s.setVoteIDStatus(agg.ProcessID, ballot.VoteID, VoteIDStatusError); err != nil {
			log.Warnw("failed to set vote ID status to error", "error", err.Error())
		}
	}

	// Only update process stats for ballots that were actually aggregated
	if validAggregatedCount > 0 {
		// Update process stats: reverse the aggregation
		if err := s.updateProcessStats(agg.ProcessID, []ProcessStatsUpdate{
			{TypeStats: types.TypeStatsAggregatedVotes, Delta: -validAggregatedCount},
			{TypeStats: types.TypeStatsCurrentBatchSize, Delta: validAggregatedCount}, // restore current batch size
		}); err != nil {
			log.Warnw("failed to update process stats after batch failure",
				"error", err.Error(),
				"processID", fmt.Sprintf("%x", agg.ProcessID),
				"validAggregatedCount", validAggregatedCount,
				"totalBatchSize", len(agg.Ballots),
			)
		}
	}

	// Remove the reservation
	if err := s.deleteArtifact(aggregBatchReservPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete reservation: %w", err)
	}
	// Remove the batch from the aggregator queue
	if err := s.deleteArtifact(aggregBatchPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete aggregator batch: %w", err)
	}
	return nil
}

// NextAggregatorBatch returns the next aggregated ballot batch for a given
// processID, sets a reservation.
// Returns ErrNoMoreElements if no more elements are available.
func (s *Storage) NextAggregatorBatch(processID []byte) (*AggregatorBallotBatch, []byte, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	pr := prefixeddb.NewPrefixedReader(s.db, aggregBatchPrefix)
	var chosenKey, chosenVal []byte
	if err := pr.Iterate(processID, func(k, v []byte) bool {
		// Append the processID prefix to the key if missing (depends on the database implementation)
		if len(k) < len(processID) || !bytes.Equal(k[:len(processID)], processID) {
			k = append(processID, k...)
		}
		if s.isReserved(aggregBatchReservPrefix, k) {
			return true
		}
		chosenKey = k
		chosenVal = v
		return false
	}); err != nil {
		return nil, nil, fmt.Errorf("iterate agg batches: %w", err)
	}
	if chosenVal == nil {
		return nil, nil, ErrNoMoreElements
	}

	var abb AggregatorBallotBatch
	if err := DecodeArtifact(chosenVal, &abb); err != nil {
		return nil, nil, fmt.Errorf("decode agg batch: %w", err)
	}

	if err := s.setReservation(aggregBatchReservPrefix, chosenKey); err != nil {
		return nil, nil, ErrNoMoreElements
	}

	return &abb, chosenKey, nil
}

// MarkAggregatorBatchPending moves an aggregator batch to the pending state.
// This is used when an aggregator batch needs to be retried or reprocessed.
// It ensures that the batch is stored in the pending queue and can be picked
// up for processing again. If the batch already exists in the pending queue,
// it returns ErrKeyAlreadyExists. Only one pending batch per process is
// allowed.
func (s *Storage) MarkAggregatorBatchPending(batch *AggregatorBallotBatch) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	val, err := EncodeArtifact(batch)
	if err != nil {
		return fmt.Errorf("encode batch: %w", err)
	}
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), pendingAggregBatchPrefix)
	key := hashKey(val)
	// Check if already exists
	if _, err := wTx.Get(append(slices.Clone(batch.ProcessID), key...)); err == nil {
		wTx.Discard()
		return ErrKeyAlreadyExists
	}

	if err := wTx.Set(append(slices.Clone(batch.ProcessID), key...), val); err != nil {
		wTx.Discard()
		return err
	}
	return wTx.Commit()
}

// PendingAggregatorBatch retrieves a pending aggregator batch for a given
// processID. If no pending batch is found, it returns ErrNotFound.
func (s *Storage) PendingAggregatorBatch(processID []byte) (*AggregatorBallotBatch, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	pr := prefixeddb.NewPrefixedReader(s.db, pendingAggregBatchPrefix)
	var chosenVal []byte
	if err := pr.Iterate(processID, func(_, v []byte) bool {
		chosenVal = v
		return false
	}); err != nil {
		return nil, fmt.Errorf("iterate pending agg batches: %w", err)
	}
	if chosenVal == nil {
		return nil, ErrNotFound
	}

	var batch AggregatorBallotBatch
	if err := DecodeArtifact(chosenVal, &batch); err != nil {
		return nil, fmt.Errorf("decode pending agg batch: %w", err)
	}
	return &batch, nil
}

// MarkAggregatorBatchDone called after processing aggregator batch. For simplicity,
// we just remove it from aggregator queue and reservation.
func (s *Storage) MarkAggregatorBatchDone(k []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	if err := s.deleteArtifact(aggregBatchReservPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err := s.deleteArtifact(aggregBatchPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

// PushStateTransitionBatch pushes a state transition batch to the state transition queue.
func (s *Storage) PushStateTransitionBatch(stb *StateTransitionBatch) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// encode the state transition batch
	val, err := EncodeArtifact(stb)
	if err != nil {
		return fmt.Errorf("encode state transition batch: %w", err)
	}

	// initialize the write transaction over the state transition prefix
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), stateTransitionPrefix)

	// create the key by hashing the value
	key := hashKey(val)

	// set the key-value pair in the write transaction
	if err := wTx.Set(append(slices.Clone(stb.ProcessID), key...), val); err != nil {
		wTx.Discard()
		return err
	}

	if err := wTx.Commit(); err != nil {
		return err
	}

	// Update process stats
	if err := s.updateProcessStats(stb.ProcessID, []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsStateTransitions, Delta: 1},
	}); err != nil {
		return fmt.Errorf("failed to update process stats: %w", err)
	}

	// Update status of all vote IDs in the batch to processed
	for _, ballot := range stb.Ballots {
		if err := s.setVoteIDStatus(stb.ProcessID, ballot.VoteID, VoteIDStatusProcessed); err != nil {
			log.Warnw("failed to set vote ID status to processed", "error", err.Error())
		}
	}

	return nil
}

func (s *Storage) NextStateTransitionBatch(processID []byte) (*StateTransitionBatch, []byte, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	// initialize the read transaction over the state transition prefix
	pr := prefixeddb.NewPrefixedReader(s.db, stateTransitionPrefix)
	var chosenKey, chosenVal []byte
	if err := pr.Iterate(processID, func(k, v []byte) bool {
		// append the processID prefix to the key if missing
		// (depends on the database implementation)
		if len(k) < len(processID) || !bytes.Equal(k[:len(processID)], processID) {
			k = append(processID, k...)
		}
		// check if reserved
		if s.isReserved(stateTransitionReservPrefix, k) {
			return true
		}
		// store the first non-reserved state transition batch
		chosenKey = k
		chosenVal = v
		return false
	}); err != nil {
		return nil, nil, fmt.Errorf("iterate state transition batches: %w", err)
	}
	// if no state transition batch is found, return nil and ErrNoMoreElements
	if chosenVal == nil {
		return nil, nil, ErrNoMoreElements
	}
	// decode the state transition batch found
	var stb StateTransitionBatch
	if err := DecodeArtifact(chosenVal, &stb); err != nil {
		return nil, nil, fmt.Errorf("decode state transition batch: %w", err)
	}
	// set reservation
	if err := s.setReservation(stateTransitionReservPrefix, chosenKey); err != nil {
		return nil, nil, ErrNoMoreElements
	}
	// return the state transition batch, the key and nil error
	return &stb, chosenKey, nil
}

func (s *Storage) MarkStateTransitionBatchDone(k []byte, pid []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Get the state transition batch before deleting it to extract vote IDs
	pr := prefixeddb.NewPrefixedReader(s.db, stateTransitionPrefix)
	val, err := pr.Get(k)
	if err != nil {
		if !errors.Is(err, db.ErrKeyNotFound) {
			return fmt.Errorf("get state transition batch: %w", err)
		}
		// If batch not found, just continue with cleanup
	} else {
		// Decode the batch to get the vote IDs
		var stb StateTransitionBatch
		if err := DecodeArtifact(val, &stb); err != nil {
			log.Warnw("failed to decode state transition batch for vote ID settlement",
				"error", err.Error(),
				"processID", fmt.Sprintf("%x", pid),
			)
		} else {
			// Extract vote IDs from the batch
			voteIDs := make([][]byte, len(stb.Ballots))
			for i, ballot := range stb.Ballots {
				voteIDs[i] = ballot.VoteID

				// Release nullifier lock
				s.releaseVoteID(ballot.VoteID.BigInt().MathBigInt())
			}

			// Mark all vote IDs in the batch as settled (using unsafe version to avoid deadlock)
			if err := s.markVoteIDsSettled(pid, voteIDs); err != nil {
				log.Warnw("failed to mark vote IDs as settled",
					"error", err.Error(),
					"processID", fmt.Sprintf("%x", pid),
					"voteIDCount", len(voteIDs),
				)
			} else {
				log.Debugw("marked vote IDs as settled",
					"processID", fmt.Sprintf("%x", pid),
					"voteIDCount", len(voteIDs),
				)
			}
		}
	}

	// Remove the reservation and the batch itself
	if err := s.removeStateTransitionBatch(k); err != nil {
		return err
	}

	// Update process stats
	if err := s.updateProcessStats(pid, []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsSettledStateTransitions, Delta: 1},
		{TypeStats: types.TypeStatsLastTransitionDate, Delta: 0},
	}); err != nil {
		log.Warnw("failed to update process stats after marking state transition batch as done",
			"error", err.Error(),
			"processID", fmt.Sprintf("%x", pid),
		)
	}

	return nil
}

// RemoveStateTransitionBatchesByProcess removes all state transition batches
// for a given processID.
func (s *Storage) RemoveStateTransitionBatchesByProcess(pid []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	// get every batch key for the processID
	batchesToRemove := []types.HexBytes{}
	pr := prefixeddb.NewPrefixedReader(s.db, stateTransitionPrefix)
	if err := pr.Iterate(pid, func(k, _ []byte) bool {
		// Append the processID prefix to the key if missing (depends on the database implementation)
		if len(k) < len(pid) || !bytes.Equal(k[:len(pid)], pid) {
			k = append(pid, k...)
		}
		batchesToRemove = append(batchesToRemove, k)
		return true
	}); err != nil {
		return fmt.Errorf("iterate state transition batches: %w", err)
	}
	// iterate over all keys to remove the reservation and the batch
	for _, k := range batchesToRemove {
		if err := s.removeStateTransitionBatch(k); err != nil {
			return err
		}
	}
	// TODO: check if we need to update stats here
	return nil
}

// MarkStateTransitionBatchOutdated marks a state transition batch as outdated,
// removes the reservation, and deletes the batch from the state transition
// queue. This is called when the Ethereum smart contract state root differs
// from the local one, indicating that the state transition proof needs to be
// regenerated. The ballots and vote IDs remain valid and keep their current
// status (processed), but the proof is outdated.
func (s *Storage) MarkStateTransitionBatchOutdated(key []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Get the state transition batch before deleting it for logging purposes
	pr := prefixeddb.NewPrefixedReader(s.db, stateTransitionPrefix)
	val, err := pr.Get(key)
	if err != nil {
		if errors.Is(err, db.ErrKeyNotFound) {
			log.Warnw("state transition batch not found during outdated marking", "key", fmt.Sprintf("%x", key))
			// Still try to clean up reservation
			if err := s.deleteArtifact(stateTransitionReservPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
				return fmt.Errorf("delete state transition reservation: %w", err)
			}
			return nil
		}
		return fmt.Errorf("get state transition batch: %w", err)
	}

	// Decode the batch to get information for logging
	var stb StateTransitionBatch
	if err := DecodeArtifact(val, &stb); err != nil {
		log.Warnw("failed to decode state transition batch for outdated marking",
			"error", err.Error(),
			"key", fmt.Sprintf("%x", key),
		)
		// Continue with cleanup even if we can't decode
	} else {
		log.Infow("marked state transition batch as outdated",
			"processID", fmt.Sprintf("%x", stb.ProcessID),
			"totalBallots", len(stb.Ballots),
			"reason", "ethereum state root mismatch")

		// Note: We don't change vote ID statuses or release nullifiers because:
		// - The ballots are still valid and remain in "processed" status
		// - The nullifiers should remain locked until the new state transition is completed
		// - Only the proof is outdated, not the underlying data
	}

	// Remove the reservation
	if err := s.deleteArtifact(stateTransitionReservPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete state transition reservation: %w", err)
	}

	// Remove the batch from the state transition queue
	if err := s.deleteArtifact(stateTransitionPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete state transition batch: %w", err)
	}

	// Release the ballot batch reservation, so the batch can be processed again
	if err := s.releaseAggregatorBatchReservation(stb.BatchID); err != nil {
		log.Warnw("failed to release ballot batch reservation after marking state transition batch as outdated",
			"error", err.Error(),
			"batchID", fmt.Sprintf("%x", stb.BatchID),
		)
	}

	return nil
}

// releaseAggregatorBatchReservation removes the reservation for an aggregated ballot batch.
func (s *Storage) releaseAggregatorBatchReservation(k []byte) error {
	if err := s.deleteArtifact(aggregBatchReservPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

// removeStateTransitionBatch is an internal helper to remove a state transition
// batch from the storage (state transition queue and reservation). It assumes
// the caller already holds the globalLock.
func (s *Storage) removeStateTransitionBatch(key []byte) error {
	// remove reservation
	if err := s.deleteArtifact(stateTransitionReservPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete state transition reservation: %w", err)
	}
	// remove from state transition queue
	if err := s.deleteArtifact(stateTransitionPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete state transition batch: %w", err)
	}
	return nil
}
