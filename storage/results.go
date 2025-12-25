package storage

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/vocdoni/arbo"
	"github.com/vocdoni/davinci-node/db/prefixeddb"
	"github.com/vocdoni/davinci-node/log"
)

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
		chosenVal = bytes.Clone(v)
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
