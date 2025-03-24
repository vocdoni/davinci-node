package storage

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"go.vocdoni.io/dvote/db/prefixeddb"
)

// PushBallot stores a new ballot into the pending ballots queue.
func (s *Storage) PushBallot(b *Ballot) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	val, err := encodeArtifact(b)
	if err != nil {
		return fmt.Errorf("encode ballot: %w", err)
	}
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), ballotPrefix)
	key := hashKey(val)
	if err := wTx.Set(key, val); err != nil {
		wTx.Discard()
		return err
	}
	return wTx.Commit()
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

// MarkBallotDone called after we have processed the ballot. We push the
// verified ballot to the next queue. In this scenario, next stage is
// verifiedBallot so we do not store the original ballot.
func (s *Storage) MarkBallotDone(k []byte, vb *VerifiedBallot) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// remove reservation
	if err := s.deleteArtifact(ballotReservationPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete reservation: %w", err)
	}

	// remove from pending queue
	if err := s.deleteArtifact(ballotPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete pending ballot: %w", err)
	}

	// store verified ballot
	val, err := encodeArtifact(vb)
	if err != nil {
		return fmt.Errorf("encode verified ballot: %w", err)
	}
	wTx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), verifiedBallotPrefix)
	// key with processID as prefix + unique portion from original key
	combKey := append(vb.ProcessID, k...)
	if err := wTx.Set(combKey, val); err != nil {
		wTx.Discard()
		return err
	}
	return wTx.Commit()
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
		key := append(processID, k...)
		if len(res) == maxCount {
			log.Warnf("max count reached")
			return false
		}
		// Skip if already reserved
		if s.isReserved(verifiedBallotReservPrefix, key) {
			return true
		}
		var vb VerifiedBallot
		if err := decodeArtifact(v, &vb); err != nil {
			return true
		}

		// Make a copy of the key to avoid any potential modification
		keyCopy := make([]byte, len(key))
		copy(keyCopy, key)
		res = append(res, &vb)
		keys = append(keys, keyCopy)
		return true
	}); err != nil {
		return nil, nil, fmt.Errorf("iterate ballots: %w", err)
	}

	for _, k := range keys {
		if err := s.setReservation(verifiedBallotReservPrefix, k); err != nil {
			log.Warnw("failed to set reservation for verified ballot", "key", hex.EncodeToString(k), "error", err.Error())
			// TODO: Remove key and res from the list
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
		key := append(processID, k...)
		if !s.isReserved(verifiedBallotReservPrefix, key) {
			count++
		}
		return true
	}); err != nil {
		log.Warnw("failed to count verified ballots", "error", err.Error())
	}
	return count
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
	if err := wTx.Set(append(abb.ProcessID, key...), val); err != nil {
		wTx.Discard()
		return err
	}
	return wTx.Commit()
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
		key := append(processID, k...)
		if s.isReserved(aggregBatchReservPrefix, key) {
			return true
		}
		chosenKey = key
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

// MarkVerifiedBallotDone removes the reservation and the verified ballot.
func (s *Storage) MarkVerifiedBallotDone(k []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// remove reservation
	if err := s.deleteArtifact(verifiedBallotReservPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete verified ballot reservation: %w", err)
	}

	// remove from verified queue
	if err := s.deleteArtifact(verifiedBallotPrefix, k); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete verified ballot: %w", err)
	}

	return nil
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
