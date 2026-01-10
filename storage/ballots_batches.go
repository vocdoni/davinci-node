package storage

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/prefixeddb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

// MaxStateTransitionAttempts defines the maximum number of attempts to process
// a state transition batch before marking it as failed.
const MaxStateTransitionAttempts = 5

// PushAggregatorBatch pushes an aggregated ballot batch to the aggregator queue.
func (s *Storage) PushAggregatorBatch(abb *AggregatorBallotBatch) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	return s.pushAggregatorBatch(abb)
}

// pushAggregatorBatch is an internal helper to push an aggregated ballot batch
// to the aggregator queue. It assumes the caller already holds the globalLock.
func (s *Storage) pushAggregatorBatch(abb *AggregatorBallotBatch) error {
	val, err := EncodeArtifact(abb)
	if err != nil {
		return fmt.Errorf("encode batch: %w", err)
	}
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), aggregBatchPrefix)
	key := hashKey(val)
	if err := wTx.Set(append(abb.ProcessID.Bytes(), key...), val); err != nil {
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
func (s *Storage) RemoveAggregatorBatchesByProcess(processID types.ProcessID) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	// get every batch key for the process
	batchesToRemove := []types.HexBytes{}
	pidBytes := processID.Bytes()
	rd := prefixeddb.NewPrefixedReader(s.db, aggregBatchPrefix)
	if err := rd.Iterate(pidBytes, func(k, _ []byte) bool {
		// Append the processID prefix to the key if missing (depends on the database implementation)
		if len(k) < len(pidBytes) || !bytes.Equal(k[:len(pidBytes)], pidBytes) {
			k = append(pidBytes, k...)
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
				"processID", agg.ProcessID.String(),
				"voteID", ballot.VoteID.String(),
				"error", err.Error())
			// Continue processing as the ballot might still be valid
			validAggregatedCount++
		} else if currentStatus == VoteIDStatusAggregated {
			// Only count ballots that were actually in aggregated status
			validAggregatedCount++
		} else {
			log.Warnw("vote ID is not in aggregated status during batch failure",
				"processID", agg.ProcessID.String(),
				"voteID", ballot.VoteID.String(),
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
				"processID", agg.ProcessID.String(),
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
func (s *Storage) NextAggregatorBatch(processID types.ProcessID) (*AggregatorBallotBatch, []byte, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	pidBytes := processID.Bytes()
	pr := prefixeddb.NewPrefixedReader(s.db, aggregBatchPrefix)
	var chosenKey, chosenVal []byte
	if err := pr.Iterate(pidBytes, func(k, v []byte) bool {
		// Append the processID prefix to the key if missing (depends on the database implementation)
		if len(k) < len(pidBytes) || !bytes.Equal(k[:len(pidBytes)], pidBytes) {
			k = append(pidBytes, k...)
		}
		if s.isReserved(aggregBatchReservPrefix, k) {
			return true
		}

		// Decode the batch to check cooldown
		var tempBatch AggregatorBallotBatch
		if err := DecodeArtifact(v, &tempBatch); err != nil {
			log.Warnw("failed to decode batch during iteration", "error", err.Error())
			return true // Skip this batch
		}

		// Check if batch is in cooldown period
		if !tempBatch.LastAttemptTime.IsZero() {
			// Calculate exponential backoff: 30s * 2^(attempts-1)
			// Attempt 1: 30s, Attempt 2: 60s, Attempt 3: 120s, Attempt 4: 240s
			// Cap at 5 minutes
			cooldownSeconds := min(30*(1<<uint(tempBatch.Attempts-1)), 300)
			cooldown := time.Duration(cooldownSeconds) * time.Second
			timeSinceLastAttempt := time.Since(tempBatch.LastAttemptTime)

			if timeSinceLastAttempt < cooldown {
				return true // Skip this batch, it's in cooldown
			}
		}

		chosenKey = slices.Clone(k)
		chosenVal = bytes.Clone(v)
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
	if _, err := wTx.Get(append(batch.ProcessID.Bytes(), key...)); err == nil {
		wTx.Discard()
		return ErrKeyAlreadyExists
	}

	if err := wTx.Set(append(batch.ProcessID.Bytes(), key...), val); err != nil {
		wTx.Discard()
		return err
	}
	return wTx.Commit()
}

// PendingAggregatorBatch retrieves a pending aggregator batch for a given
// processID. If no pending batch is found, it returns ErrNotFound.
func (s *Storage) PendingAggregatorBatch(processID types.ProcessID) (*AggregatorBallotBatch, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	return s.pendingAggregatorBatch(processID)
}

// pendingAggregatorBatch is an internal helper to retrieve a pending
// aggregator batch for a given processID. It assumes the caller already
// holds the globalLock.
func (s *Storage) pendingAggregatorBatch(processID types.ProcessID) (*AggregatorBallotBatch, error) {
	pr := prefixeddb.NewPrefixedReader(s.db, pendingAggregBatchPrefix)
	var chosenVal []byte
	if err := pr.Iterate(processID.Bytes(), func(_, v []byte) bool {
		chosenVal = bytes.Clone(v)
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

// releasePendingAggregatorBatch removes the pending aggregator batch for
// a given processID. It is used after the batch has been successfully or
// unsuccessfully processed and it needs to be retried again.
func (s *Storage) releasePendingAggregatorBatch(processID types.ProcessID) error {
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), pendingAggregBatchPrefix)
	var chosenKey []byte
	if err := wTx.Iterate(processID.Bytes(), func(k, _ []byte) bool {
		chosenKey = slices.Clone(k)
		return false
	}); err != nil {
		return fmt.Errorf("iterate pending agg batches: %w", err)
	}
	if chosenKey == nil {
		return ErrNotFound
	}
	finalKey := append(processID.Bytes(), chosenKey...)
	if err := wTx.Delete(finalKey); err != nil {
		return fmt.Errorf("delete pending agg batch: %w", err)
	}
	return wTx.Commit()
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
	if err := wTx.Set(append(stb.ProcessID.Bytes(), key...), val); err != nil {
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

func (s *Storage) NextStateTransitionBatch(processID types.ProcessID) (*StateTransitionBatch, []byte, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	// initialize the read transaction over the state transition prefix
	pr := prefixeddb.NewPrefixedReader(s.db, stateTransitionPrefix)
	var chosenKey, chosenVal []byte
	pidBytes := processID.Bytes()
	if err := pr.Iterate(pidBytes, func(k, v []byte) bool {
		// append the processID prefix to the key if missing
		// (depends on the database implementation)
		if len(k) < len(pidBytes) || !bytes.Equal(k[:len(pidBytes)], pidBytes) {
			k = append(pidBytes, k...)
		}
		// check if reserved
		if s.isReserved(stateTransitionReservPrefix, k) {
			return true
		}
		// store the first non-reserved state transition batch
		chosenKey = slices.Clone(k)
		chosenVal = bytes.Clone(v)
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

func (s *Storage) MarkStateTransitionBatchDone(k []byte, processID types.ProcessID) error {
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
				"processID", processID.String(),
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
			if err := s.markVoteIDsSettled(processID, voteIDs); err != nil {
				log.Warnw("failed to mark vote IDs as settled",
					"error", err.Error(),
					"processID", processID.String(),
					"voteIDCount", len(voteIDs),
				)
			} else {
				log.Debugw("marked vote IDs as settled",
					"processID", processID.String(),
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
	if err := s.updateProcessStats(processID, []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsSettledStateTransitions, Delta: 1},
		{TypeStats: types.TypeStatsLastTransitionDate, Delta: 0},
	}); err != nil {
		log.Warnw("failed to update process stats after marking state transition batch as done",
			"error", err.Error(),
			"processID", processID.String(),
		)
	}

	return nil
}

// RemoveStateTransitionBatchesByProcess removes all state transition batches
// for a given processID.
func (s *Storage) RemoveStateTransitionBatchesByProcess(processID types.ProcessID) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	// get every batch key for the processID
	batchesToRemove := []types.HexBytes{}
	pidBytes := processID.Bytes()
	pr := prefixeddb.NewPrefixedReader(s.db, stateTransitionPrefix)
	if err := pr.Iterate(pidBytes, func(k, _ []byte) bool {
		// Append the processID prefix to the key if missing (depends on the database implementation)
		if len(k) < len(pidBytes) || !bytes.Equal(k[:len(pidBytes)], pidBytes) {
			k = append(pidBytes, k...)
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
			"processID", stb.ProcessID.String(),
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

// MarkStateTransitionBatchFailed marks a state transition batch as failed,
// sets all ballots in the batch to error status, removes the reservation,
// and deletes the batch from the state transition queue. This is typically
// called when the state transition processing fails or is not valid.
func (s *Storage) MarkStateTransitionBatchFailed(key []byte, processID types.ProcessID) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	pr := prefixeddb.NewPrefixedReader(s.db, stateTransitionPrefix)
	rawBatch, err := pr.Get(key)
	if err != nil {
		if errors.Is(err, db.ErrKeyNotFound) {
			return nil
		}
		return fmt.Errorf("get state transition batch: %w", err)
	}
	var stb StateTransitionBatch
	if err := DecodeArtifact(rawBatch, &stb); err != nil {
		return fmt.Errorf("decode state transition batch: %w", err)
	}

	// Remove the state transition batch any way
	defer func() {
		if err := s.removeStateTransitionBatch(key); err != nil {
			log.Errorw(err, "failed to remove failed state transition batch")
		}
		// Release pending tx
		if err := s.prunePendingTx(StateTransitionTx, processID); err != nil {
			log.Warnw("failed to release pending tx",
				"error", err,
				"processID", processID.String())
		}
	}()

	if pendingBatch, err := s.pendingAggregatorBatch(processID); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("failed to get pending aggregator batch: %w", err)
	} else if pendingBatch != nil {
		// Release pending aggregator batch to be able to reprocess it
		if err := s.releasePendingAggregatorBatch(processID); err != nil {
			return fmt.Errorf("failed to release pending aggregator batch: %w", err)
		}

		// Increment attempts counter and check if max attempts reached
		pendingBatch.Attempts++
		if pendingBatch.Attempts >= MaxStateTransitionAttempts {
			log.Warnw("maximum state transition attempts reached for pending aggregator batch",
				"processID", processID.String(),
				"attempts", pendingBatch.Attempts)
			// Mark all ballots in the batch as error
			for _, v := range stb.Ballots {
				if err := s.setVoteIDStatus(processID, v.VoteID, VoteIDStatusError); err != nil {
					log.Warnw("failed to set vote ID status to failed", "error", err.Error())
				}
			}
			return nil
		}

		// Validate that the process is still accepting votes before retrying
		process, err := s.process(stb.ProcessID)
		if err != nil {
			return fmt.Errorf("failed to get process for validation: %w", err)
		}
		if isAccepting, err := s.processIsAcceptingVotes(stb.ProcessID, process); err != nil || !isAccepting {
			log.Warnw("process is no longer accepting votes, marking batch as permanently failed",
				"processID", stb.ProcessID.String(),
				"error", err)
			// Mark all ballots in the batch as error
			for _, v := range stb.Ballots {
				if err := s.setVoteIDStatus(stb.ProcessID, v.VoteID, VoteIDStatusError); err != nil {
					log.Warnw("failed to set vote ID status to failed", "error", err.Error())
				}
			}
			return nil
		}

		if process.StateRoot == nil {
			return fmt.Errorf("process %s has no state root", processID.String())
		}

		// Load the current state with the latest root
		currentState, err := state.New(s.StateDB(), stb.ProcessID)
		if err != nil {
			return fmt.Errorf("failed to load state for process %s: %w", stb.ProcessID.String(), err)
		}
		if err := currentState.SetRootAsBigInt(process.StateRoot.MathBigInt()); err != nil {
			return fmt.Errorf("failed to set latest state root for process %s: %w", stb.ProcessID.String(), err)
		}

		// Check which votes are already in the state and filter them out
		validBallots := make([]*AggregatorBallot, 0, len(pendingBatch.Ballots))
		for _, v := range stb.Ballots {
			if currentState.ContainsVoteID(v.VoteID.BigInt().MathBigInt()) {
				log.Debugw("vote already in state, marking as failed",
					"processID", stb.ProcessID.String(),
					"voteID", v.VoteID.String())
				if err := s.setVoteIDStatus(stb.ProcessID, v.VoteID, VoteIDStatusError); err != nil {
					log.Warnw("failed to set vote ID status to failed", "error", err.Error())
				}
			} else {
				// Vote is still valid, keep it in the batch
				validBallots = append(validBallots, v)
			}
		}

		// If no valid ballots remain, don't retry
		if len(validBallots) == 0 {
			log.Infow("no valid ballots remaining after filtering, batch not retried",
				"processID", stb.ProcessID.String())
			return nil
		}

		// Update the pending batch with only valid ballots
		pendingBatch.Ballots = validBallots

		// Set LastAttemptTime to implement cooldown
		pendingBatch.LastAttemptTime = time.Now()
		if err := s.pushAggregatorBatch(pendingBatch); err != nil {
			return fmt.Errorf("failed to recover pending aggregator batch: %w", err)
		}
		log.Infow("re-pushed aggregator batch for retry with cooldown",
			"processID", stb.ProcessID.String(),
			"attempts", pendingBatch.Attempts,
			"validBallots", len(validBallots),
			"lastAttemptTime", pendingBatch.LastAttemptTime.Format(time.RFC3339))
		return nil
	}
	// If there is not pending batch or some of their votes are already in the
	// state we cannot re-push the batch, we need to mark the votes as failed.
	for _, v := range stb.Ballots {
		if err := s.setVoteIDStatus(processID, v.VoteID, VoteIDStatusError); err != nil {
			log.Warnw("failed to set vote ID status to failed", "error", err.Error())
		}
	}
	log.Warnw("batch can not be recovered after state transition failure",
		"processID", processID.String(),
		"batchID", hex.EncodeToString(key))
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
