package storage

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/vocdoni/arbo"
	"github.com/vocdoni/davinci-node/db/prefixeddb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

/*
	dbPrefix = vs
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
	// Check if the ballot is already processing
	if processing := s.IsVoteIDProcessing(b.VoteID.BigInt().MathBigInt()); processing {
		return ErrNullifierProcessing
	}

	val, err := EncodeArtifact(b)
	if err != nil {
		return fmt.Errorf("encode ballot: %w", err)
	}
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), ballotPrefix)
	if _, err := wTx.Get(b.VoteID); err == nil {
		wTx.Discard()
		return ErroBallotAlreadyExists
	}
	if err := wTx.Set(b.VoteID, val); err != nil {
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
			"voteID", hex.EncodeToString(b.VoteID),
		)
	}

	// Lock the ballot nullifier to prevent overwrites until processing is done.
	s.lockVoteID(b.VoteID.BigInt().MathBigInt())

	// Set vote ID status to pending
	return s.setVoteIDStatus(b.ProcessID, b.VoteID, VoteIDStatusPending)
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
	voteID := b.VoteID

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

// removeBallot is an internal helper to remove a ballot from the pending queue.
// It assumes the caller already holds the globalLock.
func (s *Storage) removeBallot(pid, voteID []byte) error {
	// remove reservation
	if err := s.deleteArtifact(ballotReservationPrefix, voteID); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("error deleting reservation: %w", err)
	}
	// remove from pending queue
	if err := s.deleteArtifact(ballotPrefix, voteID); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("error deleting ballot: %w", err)
	}
	// update process stats
	if err := s.updateProcessStats(pid, []ProcessStatsUpdate{
		{TypeStats: types.TypeStatsPendingVotes, Delta: -1},
	}); err != nil {
		log.Warnw("failed to update process stats after removing ballot",
			"error", err.Error(),
			"processID", fmt.Sprintf("%x", pid),
			"voteID", hex.EncodeToString(voteID),
		)
	}
	return nil
}

// RemoveBallot removes a ballot from the pending queue and its reservation.
func (s *Storage) RemoveBallot(processID, voteID []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	// remove the ballot stuff
	if err := s.removeBallot(processID, voteID); err != nil {
		return err
	}
	// Update vote ID status to error
	return s.setVoteIDStatus(processID, voteID, VoteIDStatusError)
}

// RemovePendingBallotsByProcess removes all pending ballots for a given process ID.
func (s *Storage) RemovePendingBallotsByProcess(pid []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// get every vote ID for the process
	votesToRemove := []types.HexBytes{}
	pr := prefixeddb.NewPrefixedReader(s.db, ballotPrefix)
	if err := pr.Iterate(nil, func(k, v []byte) bool {
		// Make a copy of the key to avoid slice reuse issues from the iterator
		keyCopy := make([]byte, len(k))
		copy(keyCopy, k)
		votesToRemove = append(votesToRemove, keyCopy)
		return true
	}); err != nil {
		return fmt.Errorf("iterate ballots: %w", err)
	}

	// iterate over the vote IDs to remove them and release their vote ID locks
	for _, voteID := range votesToRemove {
		if err := s.removeBallot(pid, voteID); err != nil {
			return err
		}
		s.releaseVoteID(voteID.BigInt().MathBigInt())
	}
	return nil
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

	// Update vote ID status to verified
	return s.setVoteIDStatus(vb.ProcessID, voteID, VoteIDStatusVerified)
}

// PullVerifiedBallots returns a list of non-reserved verified ballots for a
// given processID and creates reservations for them. The numFields parameter is
// used to limit the number of results. If no ballots are available, returns
// ErrNotFound.
func (s *Storage) PullVerifiedBallots(processID []byte, numFields int) ([]*VerifiedBallot, [][]byte, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	if numFields == 0 {
		return []*VerifiedBallot{}, nil, nil
	}

	rd := prefixeddb.NewPrefixedReader(s.db, verifiedBallotPrefix)
	var res []*VerifiedBallot
	var keys [][]byte
	if err := rd.Iterate(processID, func(k, v []byte) bool {
		// Check if we've already reached the maximum count
		if len(res) >= numFields {
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
		// Continue iteration if we haven't reached numFields
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

// removeVerifiedBallot is an internal helper to remove a verified ballot from
// the storage (verified queue and reservation). It assumes the caller already
// holds the globalLock.
func (s *Storage) removeVerifiedBallot(key []byte) error {
	// remove reservation
	if err := s.deleteArtifact(verifiedBallotReservPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete verified ballot reservation: %w", err)
	}
	// remove from verified queue
	if err := s.deleteArtifact(verifiedBallotPrefix, key); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete verified ballot: %w", err)
	}
	return nil
}

// RemoveVerifiedBallotsByProcess removes all verified ballots for a given
// processID.
func (s *Storage) RemoveVerifiedBallotsByProcess(processID []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	votesToRemove := []types.HexBytes{}
	rd := prefixeddb.NewPrefixedReader(s.db, verifiedBallotPrefix)
	if err := rd.Iterate(processID, func(k, _ []byte) bool {
		// Ensure we work on a stable copy of the key (iterator may reuse the slice)
		keyCopy := make([]byte, len(k))
		copy(keyCopy, k)
		// Append the processID prefix to the key if missing (depends on the database implementation)
		if len(keyCopy) < len(processID) || !bytes.Equal(keyCopy[:len(processID)], processID) {
			keyCopy = append(processID, keyCopy...)
		}
		votesToRemove = append(votesToRemove, keyCopy)
		return true
	}); err != nil {
		log.Warnw("failed to count verified ballots", "error", err.Error())
	}
	// iterate over all keys to remove the reservation and the verified ballot
	for _, k := range votesToRemove {
		if err := s.removeVerifiedBallot(k); err != nil {
			return err
		}
	}
	// TODO: check if we need to update process stats here
	return nil
}

// MarkVerifiedBallotsDone removes the reservation and the verified ballots.
// It removes the verified ballots from the verified ballots queue and deletes
// their reservations.
func (s *Storage) MarkVerifiedBallotsDone(keys ...[]byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Iterate over all keys to remove the reservation and the verified ballot
	for _, k := range keys {
		if err := s.removeVerifiedBallot(k); err != nil {
			return err
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

		// Check current vote ID status to avoid double-processing
		currentStatus, err := s.voteIDStatusUnsafe(ballot.ProcessID, ballot.VoteID)
		if err != nil {
			log.Warnw("could not get vote ID status during failure marking",
				"processID", fmt.Sprintf("%x", ballot.ProcessID),
				"voteID", hex.EncodeToString(ballot.VoteID),
				"error", err.Error())
			// Continue processing as the ballot might still be valid
		} else if currentStatus != VoteIDStatusVerified {
			log.Warnw("vote ID is not in verified status, skipping counter updates",
				"processID", fmt.Sprintf("%x", ballot.ProcessID),
				"voteID", hex.EncodeToString(ballot.VoteID),
				"currentStatus", VoteIDStatusName(currentStatus))
			// Still remove the ballot from verified queue but don't update counters
		} else {
			// Only count ballots that were actually in verified status
			processKey := string(ballot.ProcessID)
			processBallots[processKey] = append(processBallots[processKey], *ballot)
		}

		// Mark the vote ID as error
		if err := s.setVoteIDStatus(ballot.ProcessID, ballot.VoteID, VoteIDStatusError); err != nil {
			return fmt.Errorf("set vote ID status to error: %w", err)
		}

		// Remove verified ballot and its reservation
		if err := s.removeVerifiedBallot(k); err != nil {
			return fmt.Errorf("remove verified ballot: %w", err)
		}

		// Release nullifier lock
		s.releaseVoteID(ballot.VoteID.BigInt().MathBigInt())
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

	// Update status of all vote IDs in the batch to aggregated
	// TODO: this should use a single write transaction
	for _, ballot := range abb.Ballots {
		if err := s.setVoteIDStatus(abb.ProcessID, ballot.VoteID, VoteIDStatusAggregated); err != nil {
			log.Warnw("failed to set vote ID status to aggregated", "error", err.Error())
		}
	}

	return nil
}

// RemoveBallotBatchesByProcess removes all ballot batches for a given processID.
func (s *Storage) RemoveBallotBatchesByProcess(pid []byte) error {
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

func (s *Storage) MarkStateTransitionBatchDone(k []byte, pid []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Get the state transition batch before deleting it to extract vote IDs
	pr := prefixeddb.NewPrefixedReader(s.db, stateTransitionPrefix)
	val, err := pr.Get(k)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
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
	defer wTx.Discard()

	// check if the processID already exists
	if _, err := wTx.Get(res.ProcessID); err == nil {
		// raise an error if the processID already exists
		return fmt.Errorf("verified results for processID %x already exists", res.ProcessID)
	}

	// set the key-value pair in the write transaction using the processID as
	// the key
	if err := wTx.Set(res.ProcessID, val); err != nil {
		return err
	}

	return wTx.Commit()
}

// NextVerifiedResults retrieves the next verified results from the storage.
// It does not make any reservations, so its up to the calle to ensure that
// the results are processed and marked as verified before calling this function
// again. It returns the next verified results or ErrNoMoreElements if there
// are no more verified results available.
func (s *Storage) NextVerifiedResults() (*VerifiedResults, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	pr := prefixeddb.NewPrefixedReader(s.db, verifiedResultPrefix)
	var chosenVal []byte
	if err := pr.Iterate(nil, func(k, v []byte) bool {
		log.Debugw("found verified result entry", "key", hex.EncodeToString(k), "keyLen", len(k))
		chosenVal = v
		return false
	}); err != nil {
		return nil, fmt.Errorf("iterate verified results: %w", err)
	}
	if chosenVal == nil {
		return nil, ErrNoMoreElements
	}
	var res VerifiedResults
	if err := DecodeArtifact(chosenVal, &res); err != nil {
		return nil, fmt.Errorf("decode verified results: %w", err)
	}

	log.Debugw("retrieved verified results from storage",
		"processID", hex.EncodeToString(res.ProcessID))

	// Return the verified results
	return &res, nil
}

// MarkVerifiedResultsDone marks the results for a given processID as verified.
func (s *Storage) MarkVerifiedResultsDone(processID []byte) error {
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

// HasVerifiedResults checks if verified results exist for a given processID.
// This is used to prevent re-generation of results that have already been
// generated but may have failed to upload to the contract.
func (s *Storage) HasVerifiedResults(processID []byte) bool {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	pr := prefixeddb.NewPrefixedReader(s.db, verifiedResultPrefix)
	_, err := pr.Get(processID)
	return err == nil
}
