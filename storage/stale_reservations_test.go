package storage

import (
	"path/filepath"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/metadb"
)

func TestReleaseStaleReservations(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	testDB, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(testDB)
	defer st.Close()

	// Test 1: Create some reservations manually
	testKey1 := []byte("testkey1")
	testKey2 := []byte("testkey2")
	testKey3 := []byte("testkey3")

	// Create reservations with different ages
	now := time.Now().Unix()

	// Fresh reservation (should not be released)
	freshReservation := &reservationRecord{Timestamp: now}
	freshData, err := EncodeArtifact(freshReservation)
	c.Assert(err, qt.IsNil)

	// Old reservation (should be released)
	oldReservation := &reservationRecord{Timestamp: now - 15*60} // 15 minutes ago
	oldData, err := EncodeArtifact(oldReservation)
	c.Assert(err, qt.IsNil)

	// Very old reservation (should be released)
	veryOldReservation := &reservationRecord{Timestamp: now - 30*60} // 30 minutes ago
	veryOldData, err := EncodeArtifact(veryOldReservation)
	c.Assert(err, qt.IsNil)

	// Set reservations directly in the database
	err = st.setTestReservation(ballotReservationPrefix, testKey1, freshData)
	c.Assert(err, qt.IsNil)
	err = st.setTestReservation(ballotReservationPrefix, testKey2, oldData)
	c.Assert(err, qt.IsNil)
	err = st.setTestReservation(verifiedBallotReservPrefix, testKey3, veryOldData)
	c.Assert(err, qt.IsNil)

	// Verify all reservations exist
	c.Assert(st.isReserved(ballotReservationPrefix, testKey1), qt.IsTrue)
	c.Assert(st.isReserved(ballotReservationPrefix, testKey2), qt.IsTrue)
	c.Assert(st.isReserved(verifiedBallotReservPrefix, testKey3), qt.IsTrue)

	// Test 2: Release stale reservations (older than 10 minutes)
	err = st.releaseStaleReservations(10 * time.Minute)
	c.Assert(err, qt.IsNil)

	// Test 3: Verify results
	// Fresh reservation should still exist
	c.Assert(st.isReserved(ballotReservationPrefix, testKey1), qt.IsTrue)

	// Old reservations should be gone
	c.Assert(st.isReserved(ballotReservationPrefix, testKey2), qt.IsFalse)
	c.Assert(st.isReserved(verifiedBallotReservPrefix, testKey3), qt.IsFalse)
}

func TestReleaseStaleReservationsAllPrefixes(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	testDB, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(testDB)
	defer st.Close()

	// Create old reservations in all prefixes
	testKey := []byte("testkey")
	now := time.Now().Unix()
	oldReservation := &reservationRecord{Timestamp: now - 15*60} // 15 minutes ago
	oldData, err := EncodeArtifact(oldReservation)
	c.Assert(err, qt.IsNil)

	prefixes := [][]byte{
		ballotReservationPrefix,
		verifiedBallotReservPrefix,
		aggregBatchReservPrefix,
		stateTransitionReservPrefix,
	}

	// Set old reservations in all prefixes
	for _, prefix := range prefixes {
		err = st.setTestReservation(prefix, testKey, oldData)
		c.Assert(err, qt.IsNil)
		c.Assert(st.isReserved(prefix, testKey), qt.IsTrue)
	}

	// Release stale reservations
	err = st.releaseStaleReservations(10 * time.Minute)
	c.Assert(err, qt.IsNil)

	// Verify all old reservations are gone
	for _, prefix := range prefixes {
		c.Assert(st.isReserved(prefix, testKey), qt.IsFalse)
	}
}

func TestReleaseStaleReservationsInvalidData(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	testDB, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(testDB)
	defer st.Close()

	// Create reservation with invalid data (should be treated as stale)
	testKey := []byte("invalidkey")
	invalidData := []byte("invalid reservation data")

	err = st.setTestReservation(ballotReservationPrefix, testKey, invalidData)
	c.Assert(err, qt.IsNil)
	c.Assert(st.isReserved(ballotReservationPrefix, testKey), qt.IsTrue)

	// Release stale reservations
	err = st.releaseStaleReservations(10 * time.Minute)
	c.Assert(err, qt.IsNil)

	// Invalid reservation should be removed
	c.Assert(st.isReserved(ballotReservationPrefix, testKey), qt.IsFalse)
}

func TestRecoverClearsAllReservations(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	testDB, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(testDB)
	defer st.Close()

	// Create reservations in all prefixes
	testKey := []byte("testkey")
	now := time.Now().Unix()
	reservation := &reservationRecord{Timestamp: now}
	data, err := EncodeArtifact(reservation)
	c.Assert(err, qt.IsNil)

	prefixes := [][]byte{
		ballotReservationPrefix,
		verifiedBallotReservPrefix,
		aggregBatchReservPrefix,
		stateTransitionReservPrefix,
	}

	// Set reservations in all prefixes
	for _, prefix := range prefixes {
		err = st.setTestReservation(prefix, testKey, data)
		c.Assert(err, qt.IsNil)
		c.Assert(st.isReserved(prefix, testKey), qt.IsTrue)
	}

	// Call recover (simulating restart)
	err = st.recover()
	c.Assert(err, qt.IsNil)

	// Verify all reservations are gone
	for _, prefix := range prefixes {
		c.Assert(st.isReserved(prefix, testKey), qt.IsFalse)
	}
}

// Helper function to set test reservations directly
func (s *Storage) setTestReservation(prefix, key, data []byte) error {
	wTx := s.db.WriteTx()
	defer wTx.Discard()

	prefixedKey := append(prefix, key...)
	if err := wTx.Set(prefixedKey, data); err != nil {
		return err
	}
	return wTx.Commit()
}
