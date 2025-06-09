package storage

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/vocdoni/arbo"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"go.vocdoni.io/dvote/db/prefixeddb"
)

/*
	dbPrefix = bs
	processID_voteID = status
	processID_voteID = 0 -> pending
	processID_voteID = 1 -> verified
	processID_voteID = 2 -> aggregated
	processID_voteID = 3 -> processed
	processID_voteID = 4 -> settled
	processID_voteID = 5 -> error
*/

// ErroBallotAlreadyExists is returned when a ballot already exists in the pending queue.
var ErroBallotAlreadyExists = errors.New("ballot already exists")

// PushBallot stores a new ballot into the pending ballots queue.
func (s *Storage) PushBallot(b *Ballot) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	val, err := EncodeArtifact(b)
	if err != nil {
		return fmt.Errorf("encode ballot: %w", err)
	}
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), ballotPrefix)
	if _, err := wTx.Get(b.VoteID()); err == nil {
		wTx.Discard()
		return ErroBallotAlreadyExists
	}
	if err := wTx.Set(b.VoteID(), val); err != nil {
		wTx.Discard()
		return err
	}
	if err := wTx.Commit(); err != nil {
		return err
	}

	// Update process stats
	if err := s.updateProcessStats(b.ProcessID, []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsPendingVotes, Delta: 1},
	}); err != nil {
		log.Warnw("failed to update process stats after pushing ballot",
			"error", err.Error(),
			"processID", fmt.Sprintf("%x", b.ProcessID),
			"voteID", hex.EncodeToString(b.VoteID()),
		)
	}

	// Set ballot status to pending
	return s.setBallotStatus(b.ProcessID, b.VoteID(), BallotStatusPending)
}

// NextBallot returns the next non-reserved ballot, creates a reservation, and
// returns it. It returns the ballot, the key, and an error. If no ballots are
// available, returns ErrNoMoreElements. The key is used to mark the ballot as
// done after processing and to pass it to the next stage.
func (s *Storage) NextBallot() (*Ballot, []byte, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	s.workersLock.Lock()
	defer s.workersLock.Unlock()
	return s.nextBallot()
}

// NextBallotForWorker is like NextBallot but does not lock the global lock.
// It is used by workers to fetch the next ballot without blocking other operations.
func (s *Storage) NextBallotForWorker() (*Ballot, []byte, error) {
	s.workersLock.Lock()
	defer s.workersLock.Unlock()
	return s.nextBallot()
}

func (s *Storage) nextBallot() (*Ballot, []byte, error) {
	pr := prefixeddb.NewPrefixedReader(s.db, ballotPrefix)
	var chosenKey, chosenVal []byte
	if err := pr.Iterate(nil, func(k, v []byte) bool {
		// check if reserved
		if s.isReserved(ballotReservationPrefix, k) {
			return true
		}
		// Make a copy of the key to avoid potential issues with slice reuse
		chosenKey = make([]byte, len(k))
		copy(chosenKey, k)
		chosenVal = v
		return false
	}); err != nil {
		return nil, nil, fmt.Errorf("iterate ballots: %w", err)
	}
	if chosenVal == nil {
		return nil, nil, ErrNoMoreElements
	}

	var b Ballot
	if err := DecodeArtifact(chosenVal, &b); err != nil {
		return nil, nil, fmt.Errorf("decode ballot: %w", err)
	}

	// The key must match the ballot's VoteID
	// When using prefixed iteration, ensure we use the ballot's actual VoteID as the key
	voteID := b.VoteID()

	// Verify that the chosen key matches the ballot's VoteID
	if !bytes.Equal(chosenKey, voteID) {
		// This should not happen, but if it does, use the ballot's VoteID as the correct key
		chosenKey = voteID
	}

	// set reservation
	if err := s.setReservation(ballotReservationPrefix, chosenKey); err != nil {
		return nil, nil, ErrNoMoreElements
	}

	return &b, chosenKey, nil
}

// RemoveBallot removes a ballot from the pending queue and its reservation.
func (s *Storage) RemoveBallot(processID, voteID []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Remove reservation
	if err := s.deleteArtifact(ballotReservationPrefix, voteID); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete reservation: %w", err)
	}

	// Remove from pending queue
	if err := s.deleteArtifact(ballotPrefix, voteID); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete pending ballot: %w", err)
	}

	// Update process stats
	if err := s.updateProcessStats(processID, []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsPendingVotes, Delta: -1},
	}); err != nil {
		log.Warnw("failed to update process stats after removing ballot",
			"error", err.Error(),
			"processID", fmt.Sprintf("%x", processID),
			"voteID", hex.EncodeToString(voteID),
		)
	}

	// Update ballot status to error
	return s.setBallotStatus(processID, voteID, BallotStatusError)
}

// ReleaseBallotReservation removes the reservation for a ballot.
func (s *Storage) ReleaseBallotReservation(voteID []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Remove reservation
	if err := s.deleteArtifact(ballotReservationPrefix, voteID); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete reservation: %w", err)
	}

	return nil
}

// MarkBallotDone called after we have processed the ballot. We push the
// verified ballot to the next queue. In this scenario, next stage is
// verifiedBallot so we do not store the original ballot.
func (s *Storage) MarkBallotDone(voteID []byte, vb *VerifiedBallot) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Remove reservation
	if err := s.deleteArtifact(ballotReservationPrefix, voteID); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete reservation: %w", err)
	}

	// Remove from pending queue
	if err := s.deleteArtifact(ballotPrefix, voteID); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete pending ballot: %w", err)
	}

	// store verified ballot
	val, err := EncodeArtifact(vb)
	if err != nil {
		return fmt.Errorf("encode verified ballot: %w", err)
	}
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), verifiedBallotPrefix)
	// key with processID as prefix + unique portion from original key
	combKey := append(slices.Clone(vb.ProcessID), voteID...)
	if err := wTx.Set(combKey, val); err != nil {
		wTx.Discard()
		return err
	}
	if err := wTx.Commit(); err != nil {
		return err
	}

	// Update process stats
	if err := s.updateProcessStats(vb.ProcessID, []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsVerifiedVotes, Delta: 1},
		{TypeStats: types.TypeStatsPendingVotes, Delta: -1},
		{TypeStats: types.TypeStatsCurrentBatchSize, Delta: 1},
	}); err != nil {
		return fmt.Errorf("failed to update process stats: %w", err)
	}

	// Update ballot status to verified
	return s.setBallotStatus(vb.ProcessID, voteID, BallotStatusVerified)
}

// PullVerifiedBallots returns a list of non-reserved verified ballots for a
// given processID and creates reservations for them. The maxCount parameter is
// used to limit the number of results. If no ballots are available, returns
// ErrNotFound.
func (s *Storage) PullVerifiedBallots(processID []byte, maxCount int) ([]*VerifiedBallot, [][]byte, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	if maxCount == 0 {
		return []*VerifiedBallot{}, nil, nil
	}

	rd := prefixeddb.NewPrefixedReader(s.db, verifiedBallotPrefix)
	var res []*VerifiedBallot
	var keys [][]byte
	if err := rd.Iterate(processID, func(k, v []byte) bool {
		// Check if we've already reached the maximum count
		if len(res) >= maxCount {
			return false
		}

		// Append the processID prefix to the key if missing (depends on the database implementation)
		if len(k) < len(processID) || !bytes.Equal(k[:len(processID)], processID) {
			k = append(processID, k...)
		}

		// Skip if already reserved
		if s.isReserved(verifiedBallotReservPrefix, k) {
			return true
		}

		var vb VerifiedBallot
		if err := DecodeArtifact(v, &vb); err != nil {
			return true
		}

		// Make a copy of the key to avoid any potential modification
		keyCopy := make([]byte, len(k))
		copy(keyCopy, k)
		res = append(res, &vb)
		keys = append(keys, keyCopy)
		// Continue iteration if we haven't reached maxCount
		return true
	}); err != nil {
		return nil, nil, fmt.Errorf("iterate ballots: %w", err)
	}

	// Create reservations for all the keys we're returning
	for i, k := range keys {
		if err := s.setReservation(verifiedBallotReservPrefix, k); err != nil {
			log.Warnw("failed to set reservation for verified ballot", "key", hex.EncodeToString(k), "error", err.Error())
			// Remove this key and its corresponding ballot from the results
			// since we couldn't reserve it
			if i < len(res) {
				// Remove the item at index i
				res = slices.Delete(res, i, i+1)
				keys = slices.Delete(keys, i, i+1)
			}
		}
	}

	// Return ErrNotFound if we found no ballots at all
	if len(res) == 0 {
		return nil, nil, ErrNotFound
	}

	return res, keys, nil
}

// CountPendingBallots returns the number of pending ballots in the queue
// which are not reserved. These are ballots added with PushBallot() that
// haven't been processed yet via NextBallot().
func (s *Storage) CountPendingBallots() int {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	rd := prefixeddb.NewPrefixedReader(s.db, ballotPrefix)
	count := 0
	if err := rd.Iterate(nil, func(k, _ []byte) bool {
		// Skip if already reserved
		if s.isReserved(ballotReservationPrefix, k) {
			return true
		}
		count++
		return true
	}); err != nil {
		log.Warnw("failed to count pending ballots", "error", err.Error())
	}
	return count
}

// CountVerifiedBallots returns the number of verified ballots for a given
// processID which are not reserved.
func (s *Storage) CountVerifiedBallots(processID []byte) int {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	rd := prefixeddb.NewPrefixedReader(s.db, verifiedBallotPrefix)
	count := 0
	if err := rd.Iterate(processID, func(k, _ []byte) bool {
		// Append the processID prefix to the key if missing (depends on the database implementation)
		if len(k) < len(processID) || !bytes.Equal(k[:len(processID)], processID) {
			k = append(processID, k...)
		}
		// Skip if already reserved
		if s.isReserved(verifiedBallotReservPrefix, k) {
			return true
		}
		count++
		return true
	}); err != nil {
		log.Warnw("failed to count verified ballots", "error", err.Error())
	}
	return count
}

// MarkVerifiedBallotsDone removes the reservation and the verified ballots.
// It removes the verified ballots from the verified ballots queue and deletes
// their reservations.
func (s *Storage) MarkVerifiedBallotsDone(keys ...[]byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Iterate over all keys to remove the reservation and the verified ballot
	for _, k := range keys {
		// Remove reservation
		if err := s.deleteArtifact(verifiedBallotReservPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("delete verified ballot reservation: %w", err)
		}

		// Remove from verified queue
		if err := s.deleteArtifact(verifiedBallotPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("delete verified ballot: %w", err)
		}
	}
	return nil
}

// MarkVerifiedBallotsFailed marks the verified ballots as failed, sets their
// status to error, removes their reservations, and deletes them from the
// verified ballots queue. This is typically called when the ballot processing
// fails or is not valid. It returns an error if any of the operations fail.
func (s *Storage) MarkVerifiedBallotsFailed(keys ...[]byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Group ballots by processID for efficient stats updates
	processBallots := make(map[string][]VerifiedBallot)

	// Iterate over all keys
	for _, k := range keys {
		// Retrieve the verified ballot to mark it as error
		ballot := new(VerifiedBallot)
		if err := s.getArtifact(verifiedBallotPrefix, k, ballot); err != nil {
			if errors.Is(err, ErrNotFound) {
				log.Warnw("verified ballot not found during failure marking", "key", hex.EncodeToString(k))
				continue
			}
			return fmt.Errorf("get verified ballot: %w", err)
		}

		// Check current ballot status to avoid double-processing
		currentStatus, err := s.ballotStatusUnsafe(ballot.ProcessID, ballot.VoteID)
		if err != nil {
			log.Warnw("could not get ballot status during failure marking",
				"processID", fmt.Sprintf("%x", ballot.ProcessID),
				"voteID", hex.EncodeToString(ballot.VoteID),
				"error", err.Error())
			// Continue processing as the ballot might still be valid
		} else if currentStatus != BallotStatusVerified {
			log.Warnw("ballot is not in verified status, skipping counter updates",
				"processID", fmt.Sprintf("%x", ballot.ProcessID),
				"voteID", hex.EncodeToString(ballot.VoteID),
				"currentStatus", BallotStatusName(currentStatus))
			// Still remove the ballot from verified queue but don't update counters
		} else {
			// Only count ballots that were actually in verified status
			processKey := string(ballot.ProcessID)
			processBallots[processKey] = append(processBallots[processKey], *ballot)
		}

		// Mark the ballot as error
		if err := s.setBallotStatus(ballot.ProcessID, ballot.VoteID, BallotStatusError); err != nil {
			return fmt.Errorf("set ballot status to error: %w", err)
		}

		// Remove reservation
		if err := s.deleteArtifact(verifiedBallotReservPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("delete verified ballot reservation: %w", err)
		}

		// Remove from verified queue
		if err := s.deleteArtifact(verifiedBallotPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("delete verified ballot: %w", err)
		}
	}

	// Update process stats for each process (only for ballots that were actually verified)
	for processKey, ballots := range processBallots {
		processID := []byte(processKey)
		ballotCount := len(ballots)

		if ballotCount > 0 {
			// Update process stats: decrease verified votes and current batch size
			if err := s.updateProcessStats(processID, []ProcessStatsUpdate{
				{TypeStats: types.TypeStatsVerifiedVotes, Delta: -ballotCount},
				{TypeStats: types.TypeStatsCurrentBatchSize, Delta: -ballotCount},
			}); err != nil {
				log.Warnw("failed to update process stats after marking verified ballots as failed",
					"error", err.Error(),
					"processID", fmt.Sprintf("%x", processID),
					"ballotCount", ballotCount,
				)
			}
		}
	}

	return nil
}

// PushBallotBatch pushes an aggregated ballot batch to the aggregator queue.
func (s *Storage) PushBallotBatch(abb *AggregatorBallotBatch) error {
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

	// Update status of all ballots in the batch to aggregated
	// TODO: this should use a single write transaction
	for _, ballot := range abb.Ballots {
		if err := s.setBallotStatus(abb.ProcessID, ballot.VoteID, BallotStatusAggregated); err != nil {
			log.Warnw("failed to set ballot status to aggregated", "error", err.Error())
		}
	}

	return nil
}

// MarkBallotBatchFailed marks a ballot batch as failed, sets all ballots in
// the batch to error status, removes the reservation, and deletes the batch
// from the aggregator queue. This is typically called when the batch processing
// fails or is not valid.
func (s *Storage) MarkBallotBatchFailed(key []byte) error {
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

	// Mark all ballots in the batch as error and count how many were actually aggregated
	for _, ballot := range agg.Ballots {
		// Check current ballot status to avoid double-processing
		currentStatus, err := s.ballotStatusUnsafe(agg.ProcessID, ballot.VoteID)
		if err != nil {
			log.Warnw("could not get ballot status during batch failure",
				"processID", fmt.Sprintf("%x", agg.ProcessID),
				"voteID", hex.EncodeToString(ballot.VoteID),
				"error", err.Error())
			// Continue processing as the ballot might still be valid
			validAggregatedCount++
		} else if currentStatus == BallotStatusAggregated {
			// Only count ballots that were actually in aggregated status
			validAggregatedCount++
		} else {
			log.Warnw("ballot is not in aggregated status during batch failure",
				"processID", fmt.Sprintf("%x", agg.ProcessID),
				"voteID", hex.EncodeToString(ballot.VoteID),
				"currentStatus", BallotStatusName(currentStatus))
		}

		if err := s.setBallotStatus(agg.ProcessID, ballot.VoteID, BallotStatusError); err != nil {
			log.Warnw("failed to set ballot status to error", "error", err.Error())
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

// NextBallotBatch returns the next aggregated ballot batch for a given
// processID, sets a reservation.
// Returns ErrNoMoreElements if no more elements are available.
func (s *Storage) NextBallotBatch(processID []byte) (*AggregatorBallotBatch, []byte, error) {
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

// MarkBallotBatchDone called after processing aggregator batch. For simplicity,
// we just remove it from aggregator queue and reservation.
func (s *Storage) MarkBallotBatchDone(k []byte) error {
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

	// Update status of all ballots in the batch to processed
	for _, ballot := range stb.Ballots {
		if err := s.setBallotStatus(stb.ProcessID, ballot.VoteID, BallotStatusProcessed); err != nil {
			log.Warnw("failed to set ballot status to processed", "error", err.Error())
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
	// Remove reservation
	if err := s.deleteArtifact(stateTransitionReservPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete state transition reservation: %w", err)
	}
	// Remove from state transition queue
	if err := s.deleteArtifact(stateTransitionPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete state transition batch: %w", err)
	}

	// Update process stats
	if err := s.updateProcessStats(pid, []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsSettledStateTransitions, Delta: 1},
	}); err != nil {
		log.Warnw("failed to update process stats after marking state transition batch as done",
			"error", err.Error(),
			"processID", fmt.Sprintf("%x", pid),
		)
	}

	// Update the last state transition date separately
	if err := s.setLastStateTransitionDate(pid); err != nil {
		log.Warnw("failed to update last state transition date",
			"error", err.Error(),
			"processID", fmt.Sprintf("%x", pid),
		)
	}

	return nil
}

// PushVerifiedResults stores the verified results for a given processID.
// It encodes the VerifiedResults struct and stores it in the database under
// the verifiedResultPrefix with the processID as the key. It does not
// calculate the key by the current value because the results should be unique
// for each processID. If the processID already exists, it will overwrite
// the existing value. If any error occurs during encoding or writing to the
// database, it returns an error with a descriptive message.
func (s *Storage) PushVerifiedResults(res *VerifiedResults) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// encode the verified results struct
	val, err := EncodeArtifact(res)
	if err != nil {
		return fmt.Errorf("encode state transition batch: %w", err)
	}

	// initialize the write transaction over the results prefix
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), verifiedResultPrefix)

	// set the key-value pair in the write transaction using the processID as
	// the key
	if err := wTx.Set(res.ProcessID, val); err != nil {
		wTx.Discard()
		return err
	}

	if err := wTx.Commit(); err != nil {
		return err
	}

	return nil
}

// PullVerifiedResults retrieves the verified results for a given processID.
// It initializes a read transaction over the verifiedResultPrefix and
// retrieves the value for the given processID. If the value is found, it
// decodes it into a VerifiedResults struct and returns it. If the value is
// not found, it returns ErrNotFound. If any other error occurs during the
// retrieval or decoding, it returns an error with a descriptive message.
// It also removes the reservation for verifying results if it exists.
func (s *Storage) PullVerifiedResults(processID []byte) (*VerifiedResults, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// initialize the read transaction over the results prefix
	pr := prefixeddb.NewPrefixedReader(s.db, verifiedResultPrefix)

	// get the value for the given processID
	val, err := pr.Get(processID)
	if err != nil {
		if err.Error() == arbo.ErrKeyNotFound.Error() {
			return nil, ErrNoMoreElements
		}
		return nil, fmt.Errorf("get verified results: %w", err)
	}

	// decode the verified results struct
	var res VerifiedResults
	if err := DecodeArtifact(val, &res); err != nil {
		return nil, fmt.Errorf("decode verified results: %w", err)
	}

	return &res, nil
}

func (s *Storage) MarkVerifiedResults(processID []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// initialize the read transaction over the results prefix
	tx := s.db.WriteTx()
	pr := prefixeddb.NewPrefixedWriteTx(tx, verifiedResultPrefix)
	// remove the value for the given processID
	if err := pr.Delete(processID); err != nil {
		if errors.Is(err, arbo.ErrKeyNotFound) {
			return nil
		}
		return fmt.Errorf("delete verified results: %w", err)
	}

	return tx.Commit()
}
