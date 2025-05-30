package storage

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/vocdoni/vocdoni-z-sandbox/log"
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
	val, err := encodeArtifact(b)
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

	pr := prefixeddb.NewPrefixedReader(s.db, ballotPrefix)
	var chosenKey, chosenVal []byte
	if err := pr.Iterate(nil, func(k, v []byte) bool {
		// check if reserved
		if s.isReserved(ballotReservationPrefix, k) {
			return true
		}
		chosenKey = k
		chosenVal = v
		return false
	}); err != nil {
		return nil, nil, fmt.Errorf("iterate ballots: %w", err)
	}
	if chosenVal == nil {
		return nil, nil, ErrNoMoreElements
	}

	var b Ballot
	if err := decodeArtifact(chosenVal, &b); err != nil {
		return nil, nil, fmt.Errorf("decode ballot: %w", err)
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

	return s.setBallotStatus(processID, voteID, BallotStatusError)
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
	val, err := encodeArtifact(vb)
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
		if err := decodeArtifact(v, &vb); err != nil {
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

	// Iterate over all keys
	for _, k := range keys {
		// Retrieve the verified ballot to mark it as error
		ballot := new(VerifiedBallot)
		if err := s.getArtifact(verifiedBallotPrefix, k, ballot); err != nil {
			if errors.Is(err, ErrNotFound) {
				return fmt.Errorf("verified ballot not found: %w", err)
			}
			return fmt.Errorf("get verified ballot: %w", err)
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
	return nil
}

// PushBallotBatch pushes an aggregated ballot batch to the aggregator queue.
func (s *Storage) PushBallotBatch(abb *AggregatorBallotBatch) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	val, err := encodeArtifact(abb)
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
	if err := decodeArtifact(val, agg); err != nil {
		return fmt.Errorf("decode batch: %w", err)
	}

	// Mark all ballots in the batch as error
	for _, ballot := range agg.Ballots {
		if err := s.setBallotStatus(agg.ProcessID, ballot.VoteID, BallotStatusError); err != nil {
			log.Warnw("failed to set ballot status to error", "error", err.Error())
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
	if err := decodeArtifact(chosenVal, &abb); err != nil {
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

func (s *Storage) PushStateTransitionBatch(stb *StateTransitionBatch) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// encode the state transition batch
	val, err := encodeArtifact(stb)
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
	if err := decodeArtifact(chosenVal, &stb); err != nil {
		return nil, nil, fmt.Errorf("decode state transition batch: %w", err)
	}
	// set reservation
	if err := s.setReservation(stateTransitionReservPrefix, chosenKey); err != nil {
		return nil, nil, ErrNoMoreElements
	}
	// return the state transition batch, the key and nil error
	return &stb, chosenKey, nil
}

func (s *Storage) MarkStateTransitionBatchDone(k []byte) error {
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
	return nil
}

// MarkVerifyingResultsProcess marks a process as verifying results by setting
// a reservation for the processID under the verifyingResultsReservPrefix. It
// is removed when the verify results are pulled. This is used to avoid to
// process the same results multiple times.
func (s *Storage) MarkVerifyingResultsProcess(processID []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Check if the process is already reserved for verifying results
	// If it is, we do not need to reserve it again
	if s.isReserved(verifyingResultsReservPrefix, processID) {
		return nil
	}

	// Set the process as verifying results
	if err := s.setReservation(verifyingResultsReservPrefix, processID); err != nil {
		return fmt.Errorf("set verifying results reservation: %w", err)
	}

	return nil
}

// IsVerifyingResultsProcess checks if a process is currently reserved for
// verifying results.
func (s *Storage) IsVerifyingResultsProcess(processID []byte) bool {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Check if the process is reserved for verifying results
	return s.isReserved(verifyingResultsReservPrefix, processID)
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
	val, err := encodeArtifact(res)
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
	if !s.IsVerifyingResultsProcess(processID) {
		return nil, ErrNoMoreElements
	}

	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// initialize the read transaction over the results prefix
	pr := prefixeddb.NewPrefixedReader(s.db, verifiedResultPrefix)

	// get the value for the given processID
	val, err := pr.Get(processID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get verified results: %w", err)
	}

	// decode the verified results struct
	var res VerifiedResults
	if err := decodeArtifact(val, &res); err != nil {
		return nil, fmt.Errorf("decode verified results: %w", err)
	}

	// remove the reservation for verifying results if it exists
	if err := s.deleteArtifact(verifyingResultsReservPrefix, res.ProcessID); err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("delete verifying results reservation: %w", err)
	}

	return &res, nil
}
