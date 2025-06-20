package storage

import (
	"errors"
	"fmt"
	"math/big"

	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/prefixeddb"
)

func (s *Storage) lockNullifier(nullifier *big.Int) error {
	wtx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), processingNullifierPrefix)
	defer wtx.Discard()

	// Mark as processing
	if err := wtx.Set(nullifier.Bytes(), []byte{1}); err != nil {
		return fmt.Errorf("failed to mark nullifier as processing: %w", err)
	}
	if err := wtx.Commit(); err != nil {
		return fmt.Errorf("failed to mark nullifier as processing: %w", err)
	}

	return nil
}

func (s *Storage) IsNullifierProcessing(nullifier *big.Int) (bool, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	return s.isNullifierProcessing(nullifier)
}

func (s *Storage) isNullifierProcessing(nullifier *big.Int) (bool, error) {
	reader := prefixeddb.NewPrefixedReader(s.db, processingNullifierPrefix)
	_, err := reader.Get(nullifier.Bytes())
	if err != nil {
		if errors.Is(err, db.ErrKeyNotFound) {
			return false, nil // Not processing
		}
		return false, fmt.Errorf("failed to check nullifier processing status: %w", err)
	}

	return true, nil // Is processing
}

func (s *Storage) releaseNullifier(nullifier *big.Int) error {
	wtx := prefixeddb.NewPrefixedWriteTx(s.db.WriteTx(), processingNullifierPrefix)
	defer wtx.Discard()
	if err := wtx.Delete(nullifier.Bytes()); err != nil {
		return fmt.Errorf("failed to release nullifier: %w", err)
	}
	if err := wtx.Commit(); err != nil {
		return fmt.Errorf("failed to commit nullifier release: %w", err)
	}
	return nil
}
