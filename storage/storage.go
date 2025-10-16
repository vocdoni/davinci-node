/*
Package storage provides a persistent storage layer for the Davinci node sequencer.

# Storage Organization

The storage uses a key-value database with prefixed namespaces to organize different types of data:

## Process Management
- p/  : processID → Process metadata (status, times, ballot mode, census info)
- ek/ : processID → Encryption keys (public and private keys for ballot encryption)
- md/ : metadataHash → Process metadata content (questions, choices, descriptions)

## Ballot Processing Pipeline

The ballot processing follows these stages:

1. Pending Ballots
  - b/  : voteID → Ballot (incoming ballots waiting to be verified)
  - br/ : voteID → reservation timestamp (prevents concurrent processing)

2. Verified Ballots
  - vb/ : processID + voteID → VerifiedBallot (ballots that passed verification)
  - vbr/: processID + voteID → reservation timestamp

3. Aggregated Batches
  - ag/ : processID + hash → AggregatorBallotBatch (groups of verified ballots)
  - agr/: processID + hash → reservation timestamp

4. State Transitions
  - st/ : processID + hash → StateTransitionBatch (state changes ready for chain)
  - str/: processID + hash → reservation timestamp

5. Verified Results
  - vr/ : processID → VerifiedResults (final tally results with proof)

## Vote Tracking
  - vs/ : processID + voteID → status byte
    Status values: 0=pending, 1=verified, 2=aggregated, 3=processed, 4=settled, 5=error

## Statistics
- s/  : various keys for process and global statistics
  - processID → process-specific stats
  - "totalStatsStorageKey" → global aggregated stats
  - "totalPendingBallotsKey" → total pending ballot count

## Worker Stats
- ws/ : workerAddress → WorkerStats (success/failure counts per worker)

## Pending OnChain Transactions
- ptx/: ProcessID → nil (tracks if there are pending on-chain tx for a process)

## Separate Databases
- cs_ : prefix for census database (merkle trees for voter eligibility)
- st_ : prefix for state database (merkle trees for vote state)
*/
package storage

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/prefixeddb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage/census"
)

var (
	ErrKeyAlreadyExists    = errors.New("key already exists")
	ErrNotFound            = errors.New("not found")
	ErrNoMoreElements      = errors.New("no more elements")
	ErrNullifierProcessing = errors.New("nullifier is being processed")

	// Prefixes
	ballotPrefix                = []byte("b/")
	ballotReservationPrefix     = []byte("br/")
	voteIDStatusPrefix          = []byte("vs/")
	verifiedBallotPrefix        = []byte("vb/")
	verifiedBallotReservPrefix  = []byte("vbr/")
	aggregBatchPrefix           = []byte("ag/")
	aggregBatchReservPrefix     = []byte("agr/")
	pendingAggregBatchPrefix    = []byte("pag/")
	stateTransitionPrefix       = []byte("st/")
	stateTransitionReservPrefix = []byte("str/")
	verifiedResultPrefix        = []byte("vr/")
	encryptionKeyPrefix         = []byte("ek/")
	processPrefix               = []byte("p/")
	statsPrefix                 = []byte("s/")
	metadataPrefix              = []byte("md/")
	censusDBprefix              = []byte("cs_")
	stateDBprefix               = []byte("st_")
	pendingTxPrefix             = []byte("ptx/")

	maxKeySize = 12
)

// reservationRecord stores metadata about a reservation
type reservationRecord struct {
	Timestamp int64
}

// Storage manages artifacts in various stages with reservations.
type Storage struct {
	db                db.Database
	ctx               context.Context
	cancel            context.CancelFunc
	censusDB          *census.CensusDB
	stateDB           db.Database
	globalLock        sync.Mutex              // Lock for global operations
	workersLock       sync.Mutex              // Lock for worker-related operations
	cache             *lru.Cache[string, any] // Cache for artifacts
	processingVoteIDs sync.Map                // Map to track voteIDs being processed
}

// New creates a new Storage instance.
func New(db db.Database) *Storage {
	cache, err := lru.New[string, any](1000)
	if err != nil {
		log.Fatalf("failed to create LRU cache: %v", err)
	}
	internalCtx, cancel := context.WithCancel(context.Background())
	s := &Storage{
		db:       db,
		ctx:      internalCtx,
		cancel:   cancel,
		stateDB:  prefixeddb.NewPrefixedDatabase(db, stateDBprefix),
		censusDB: census.NewCensusDB(prefixeddb.NewPrefixedDatabase(db, censusDBprefix)),
		cache:    cache,
	}

	// clear stale reservations
	if err := s.recover(); err != nil {
		log.Errorw(err, "failed to clear stale reservations")
	}

	// start monitoring for ended processes
	s.monitorEndedProcesses()

	// start monitoring for stale reservations
	s.monitorStaleReservations()

	return s
}

// recover cleans up any stale reservations and ensures that no items are
// blocked. After a crash, any reservations left behind must be cleared so that
// the corresponding ballots or aggregated batches are available for processing
// again.
func (s *Storage) recover() error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	if s.db == nil {
		return fmt.Errorf("database not initialized")
	}

	// Clear all reservations
	prefixes := [][]byte{
		ballotReservationPrefix,
		verifiedBallotReservPrefix,
		aggregBatchReservPrefix,
		stateTransitionReservPrefix,
	}

	for _, prefix := range prefixes {
		if err := s.cleanAllReservations(prefix); err != nil {
			if strings.Contains(err.Error(), "pebble: closed") {
				return fmt.Errorf("database closed")
			}
			return fmt.Errorf("failed to clear reservations for prefix %x: %w", prefix, err)
		}
	}

	// Finally, recover nullifiers that were being processed
	return s.recoverNullifiers()
}

// Close closes the storage.
func (s *Storage) Close() {
	s.cancel() // Cancel the context to stop any ongoing operations
	// recover panic to check if the database is already closed
	defer func() {
		if r := recover(); r != nil {
			if strings.Contains(fmt.Sprintf("%v", r), "closed") {
				log.Warn("storage database already closed")
				return
			}
			log.Errorf("storage close panic: %v", r)
		}
	}()
	if err := s.db.Close(); err != nil {
		fmt.Printf("failed to close storage: %v", err)
	}
}

// releaseStaleReservations checks and frees stale reservations.
func (s *Storage) releaseStaleReservations(maxAge time.Duration) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()

	now := time.Now().Unix()

	// Release stale ballot reservations
	if err := s.releaseStaleInPrefix(ballotReservationPrefix, now, maxAge); err != nil {
		return err
	}

	// Release stale verified ballot reservations
	if err := s.releaseStaleInPrefix(verifiedBallotReservPrefix, now, maxAge); err != nil {
		return err
	}

	// Release stale aggregated batch reservations
	if err := s.releaseStaleInPrefix(aggregBatchReservPrefix, now, maxAge); err != nil {
		return err
	}

	// Release stale state transition reservations
	if err := s.releaseStaleInPrefix(stateTransitionReservPrefix, now, maxAge); err != nil {
		return err
	}

	return nil
}

func (s *Storage) releaseStaleInPrefix(prefix []byte, now int64, maxAge time.Duration) error {
	wTx := prefixeddb.NewPrefixedDatabase(s.db, prefix).WriteTx()
	defer wTx.Discard()
	var staleKeys [][]byte
	if err := wTx.Iterate(nil, func(k, v []byte) bool {
		r := &reservationRecord{}
		if err := DecodeArtifact(v, r); err != nil {
			staleKeys = append(staleKeys, append([]byte(nil), k...))
			return true
		}
		if now-r.Timestamp > int64(maxAge.Seconds()) {
			staleKeys = append(staleKeys, append([]byte(nil), k...))
		}
		return true
	}); err != nil {
		return fmt.Errorf("iterate stale reservations: %w", err)
	}
	if len(staleKeys) == 0 {
		return nil
	}

	// Delete all stale keys in a single transaction
	for _, sk := range staleKeys {
		if err := wTx.Delete(sk); err != nil {
			return fmt.Errorf("delete stale reservation: %w", err)
		}
	}

	// Commit once after all deletions
	if err := wTx.Commit(); err != nil {
		return fmt.Errorf("commit stale deletion: %w", err)
	}

	if len(staleKeys) > 0 {
		log.Debugw("released stale reservations", "prefix", string(prefix), "count", len(staleKeys))
	}

	return nil
}

func (s *Storage) setReservation(prefix, key []byte) error {
	val, err := EncodeArtifact(&reservationRecord{Timestamp: time.Now().Unix()})
	if err != nil {
		return err
	}
	wTx := prefixeddb.NewPrefixedDatabase(s.db, prefix).WriteTx()
	defer wTx.Discard()
	if _, err := wTx.Get(key); err == nil {
		return ErrKeyAlreadyExists
	}
	if err := wTx.Set(key, val); err != nil {
		return err
	}
	return wTx.Commit()
}

func (s *Storage) isReserved(prefix, key []byte) bool {
	_, err := prefixeddb.NewPrefixedReader(s.db, prefix).Get(key)
	return err == nil
}

func (s *Storage) deleteArtifact(prefix, key []byte) error {
	// instance a write transaction with the prefix provided
	wTx := prefixeddb.NewPrefixedDatabase(s.db, prefix).WriteTx()
	defer wTx.Discard()
	if err := wTx.Delete(key); err != nil {
		return err
	}
	return wTx.Commit()
}

// setArtifact helper function stores any kind of artifact in the storage. It
// receives the prefix of the key, the key itself and the artifact to store. If
// the key is not provided, it generates it by hashing the artifact itself.
// It also can receive the an ArtifactEncoding format to be used for encoding,
// by default ArtifactEncodingCBOR.
func (s *Storage) setArtifact(prefix []byte, key []byte, artifact any, encoding ...ArtifactEncoding) error {
	// encode the artifact
	data, err := EncodeArtifact(artifact, encoding...)
	if err != nil {
		return err
	}
	// if the string key is provided, decode it
	if key == nil {
		hash := sha256.Sum256(data)
		key = hash[:maxKeySize]
	}

	// instance a write transaction with the prefix provided
	wTx := prefixeddb.NewPrefixedDatabase(s.db, prefix).WriteTx()
	defer wTx.Discard()

	// store the artifact in the database with the key generated
	if err := wTx.Set(key, data); err != nil {
		return err
	}
	// commit the transaction
	return wTx.Commit()
}

// getArtifact helper function retrieves any kind of artifact from the storage.
// It receives the prefix of the key and a pointer to the artifact to decode
// into. If the key is not provided, it retrieves the first artifact found for
// the prefix, and returns ErrNoMoreElements if there are no more elements.
// It also can receive the an ArtifactEncoding format to be used for decoding,
// by default ArtifactEncodingCBOR.
func (s *Storage) getArtifact(prefix []byte, key []byte, out any, encoding ...ArtifactEncoding) error {
	var data []byte
	var err error
	db := prefixeddb.NewPrefixedDatabase(s.db, prefix)
	if key != nil {
		data, err = db.Get(key)
		if err != nil {
			return ErrNotFound
		}
	} else {
		if err := db.Iterate(nil, func(_, value []byte) bool {
			data = value
			return false
		}); err != nil {
			return err
		}
		if data == nil {
			return ErrNotFound
		}
	}

	if err := DecodeArtifact(data, out, encoding...); err != nil {
		return fmt.Errorf("could not decode artifact: %w", err)
	}

	return nil
}

// listArtifacts retrieves all the keys for a given prefix.
func (s *Storage) listArtifacts(prefix []byte) ([][]byte, error) {
	var keys [][]byte
	if err := prefixeddb.NewPrefixedReader(s.db, prefix).Iterate(nil, func(k, _ []byte) bool {
		kcopy := make([]byte, len(k))
		copy(kcopy, k)
		keys = append(keys, kcopy)
		return true
	}); err != nil {
		return nil, err
	}
	return keys, nil
}

// CensusDB returns the census database instance.
func (s *Storage) CensusDB() *census.CensusDB {
	return s.censusDB
}

// StateDB returns the state database instance.
func (s *Storage) StateDB() db.Database {
	return s.stateDB
}

// monitorStaleReservations starts a goroutine that periodically checks for and
// releases stale reservations. This prevents reservations from being stuck
// indefinitely if workers crash or fail to release them properly.
func (s *Storage) monitorStaleReservations() {
	ticker := time.NewTicker(60 * time.Second) // Check every minute
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				log.Info("monitorStaleReservations stopped")
				return
			case <-ticker.C:
				if err := s.releaseStaleReservations(5 * time.Minute); err != nil {
					log.Warnw("failed to release stale reservations", "error", err)
				}
			}
		}
	}()
}

// recoverNullifiers method recovers the nullifiers that were being processed
// when the node stopped or crashed. It loads the nullifiers from verified and
// aggregated ballots to be released where they can be processed again.
//
// This method should be called after locking the global lock to ensure
// thread safety.
func (s *Storage) recoverNullifiers() error {
	// Recover nullifiers from verified ballots
	var outerErr error
	verifiedBallotsReader := prefixeddb.NewPrefixedReader(s.db, verifiedBallotPrefix)
	if err := verifiedBallotsReader.Iterate(nil, func(k, v []byte) bool {
		// Decode the verified ballot to extract voteIDs
		vb := &VerifiedBallot{}
		if err := DecodeArtifact(v, vb); err != nil {
			outerErr = fmt.Errorf("failed to decode verified ballot: %w", err)
			return false
		}
		s.lockVoteID(vb.VoteID.BigInt().MathBigInt())
		return true // Continue iterating
	}); err != nil {
		return fmt.Errorf("failed to iterate verified ballots: %w", err)
	}
	// If there was an error decoding any verified ballot, return it
	if outerErr != nil {
		return outerErr
	}

	// Recover nullifiers from aggregated batches
	aggregatedBallotsReader := prefixeddb.NewPrefixedReader(s.db, aggregBatchPrefix)
	if err := aggregatedBallotsReader.Iterate(nil, func(k, v []byte) bool {
		// Decode the aggregated ballot batch to extract voteIDs
		abb := &AggregatorBallotBatch{}
		if err := DecodeArtifact(v, abb); err != nil {
			outerErr = fmt.Errorf("failed to decode aggregated ballot batch: %w", err)
			return false // Stop iterating on error
		}
		for _, ballot := range abb.Ballots {
			s.lockVoteID(ballot.VoteID.BigInt().MathBigInt())
		}
		return true // Continue iterating
	}); err != nil {
		return fmt.Errorf("failed to iterate aggregated ballot batches: %w", err)
	}
	return outerErr
}

// lockVoteID locks a voteID to prevent concurrent processing.
func (s *Storage) lockVoteID(bigVoteID *big.Int) {
	s.processingVoteIDs.Store(bigVoteID.String(), struct{}{})
}

// IsVoteIDProcessing checks if a voteID is currently being processed.
func (s *Storage) IsVoteIDProcessing(bigVoteID *big.Int) bool {
	if _, exists := s.processingVoteIDs.Load(bigVoteID.String()); exists {
		return true // Is processing
	}
	return false // Not processing
}

// releaseVoteID releases a voteID after processing is complete or failed.
func (s *Storage) releaseVoteID(bigVoteID *big.Int) {
	s.processingVoteIDs.Delete(bigVoteID.String())
}
