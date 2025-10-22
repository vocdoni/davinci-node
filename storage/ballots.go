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

// ErroBallotAlreadyExists is returned when a ballot already exists in the
// pending queue.
var ErroBallotAlreadyExists = errors.New("ballot already exists")

// Ballot retrieves a ballot from the pending queue by its voteID. Returns the
// ballot or ErrNotFound if it doesn't exist. This is a read-only operation
// that doesn't create reservations or modify the ballot.
func (s *Storage) Ballot(voteID []byte) (*Ballot, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	pr := prefixeddb.NewPrefixedReader(s.db, ballotPrefix)
	val, err := pr.Get(voteID)
	if err != nil {
		if errors.Is(err, db.ErrKeyNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get ballot: %w", err)
	}

	var b Ballot
	if err := DecodeArtifact(val, &b); err != nil {
		return nil, fmt.Errorf("decode ballot: %w", err)
	}

	return &b, nil
}

// PushPendingBallot stores a new ballot into the pending ballots queue.
func (s *Storage) PushPendingBallot(b *Ballot) error {
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

// NextPendingBallot returns the next non-reserved ballot, creates a
// reservation, and returns it. It returns the ballot, the key, and an error.
// If no ballots are available, returns ErrNoMoreElements. The key is used to
// mark the ballot as done after processing and to pass it to the next stage.
func (s *Storage) NextPendingBallot() (*Ballot, []byte, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	s.workersLock.Lock()
	defer s.workersLock.Unlock()
	return s.nextPendingBallot()
}

// RemovePendingBallot removes a ballot from the pending queue and its reservation.
func (s *Storage) RemovePendingBallot(processID, voteID []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	// remove the ballot stuff
	if err := s.removePendingBallot(processID, voteID); err != nil {
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
		if err := s.removePendingBallot(pid, voteID); err != nil {
			return err
		}
		s.releaseVoteID(voteID.BigInt().MathBigInt())
	}
	return nil
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

// ReleasePendingBallotReservation removes the reservation for a ballot.
func (s *Storage) ReleasePendingBallotReservation(voteID []byte) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	// Remove reservation
	if err := s.deleteArtifact(ballotReservationPrefix, voteID); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete reservation: %w", err)
	}

	return nil
}

// MarkBallotVerified called after we have processed the ballot. We push the
// verified ballot to the next queue. In this scenario, next stage is
// verifiedBallot so we do not store the original ballot.
func (s *Storage) MarkBallotVerified(voteID []byte, vb *VerifiedBallot) error {
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

	// Map to track unique addresses
	addrMap := make(map[string]struct{})

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

		// Skip if address is duplicate, we only want unique addresses per batch
		if _, exists := addrMap[vb.Address.String()]; exists {
			return true
		}
		addrMap[vb.Address.String()] = struct{}{}

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

func (s *Storage) nextPendingBallot() (*Ballot, []byte, error) {
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

// removePendingBallot is an internal helper to remove a ballot from the pending queue.
// It assumes the caller already holds the globalLock.
func (s *Storage) removePendingBallot(pid, voteID []byte) error {
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
