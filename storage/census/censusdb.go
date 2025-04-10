package census

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/vocdoni/arbo"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/prefixeddb"
)

const (
	censusDBprefix          = "cs_"
	censusDBreferencePrefix = "cr_"
)

var (
	// ErrCensusNotFound is returned when a census is not found in the database.
	ErrCensusNotFound = fmt.Errorf("census not found in the local database")
	// ErrCensusAlreadyExists is returned by New() if the census already exists.
	ErrCensusAlreadyExists = fmt.Errorf("census already exists in the local database")
	// ErrWrongAuthenticationToken is returned when the authentication token is invalid.
	ErrWrongAuthenticationToken = fmt.Errorf("wrong authentication token")
	// ErrCensusIsLocked is returned if the census does not allow write operations.
	ErrCensusIsLocked = fmt.Errorf("census is locked")
	// ErrKeyNotFound is returned when a key is not found in the Merkle tree.
	ErrKeyNotFound = fmt.Errorf("key not found")

	defaultHashFunction = arbo.HashFunctionMiMC_BLS12_377
)

// updateRootRequest is used to update the root of a census tree.
type updateRootRequest struct {
	censusID uuid.UUID
	newRoot  []byte
	done     chan struct{}
}

// rootKey converts a root (a byte slice) to its canonical hexadecimal string.
func rootKey(root []byte) string {
	return hex.EncodeToString(root)
}

// CensusDB is a safe and persistent database of census trees.
// It maintains an in‑memory index mapping Merkle tree roots (in hexadecimal form)
// to census IDs.
type CensusDB struct {
	mu           sync.RWMutex
	db           db.Database
	loadedCensus map[uuid.UUID]*CensusRef
	rootIndex    map[string]uuid.UUID // maps hex(root) to censusID

	updateRootChan chan *updateRootRequest
}

// NewCensusDB creates a new CensusDB object.
// It scans the persistent database for existing census references and builds the in‑memory index.
func NewCensusDB(db db.Database) *CensusDB {
	c := &CensusDB{
		db:             db,
		loadedCensus:   make(map[uuid.UUID]*CensusRef),
		rootIndex:      make(map[string]uuid.UUID),
		updateRootChan: make(chan *updateRootRequest, 100),
	}

	// Start the root update worker.
	go func() {
		for req := range c.updateRootChan {
			if err := c.updateRoot(req.censusID, req.newRoot); err != nil {
				log.Warnw("error updating census root",
					"id", hex.EncodeToString(req.censusID[:]),
					"err", err)
			}
			if req.done != nil {
				close(req.done)
			}
		}
	}()

	return c
}

// New creates a new census and adds it to the database.
// It returns ErrCensusAlreadyExists if a census with the given ID is already present.
func (c *CensusDB) New(censusID uuid.UUID) (*CensusRef, error) {
	key := append([]byte(censusDBreferencePrefix), censusID[:]...)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check in‑memory.
	if _, exists := c.loadedCensus[censusID]; exists {
		return nil, ErrCensusAlreadyExists
	}
	// Check persistent DB.
	if _, err := c.db.Get(key); err == nil {
		return nil, ErrCensusAlreadyExists
	} else if !errors.Is(err, db.ErrKeyNotFound) {
		return nil, err
	}

	// Prepare a new census reference.
	ref := &CensusRef{
		ID:        censusID,
		MaxLevels: types.CensusTreeMaxLevels,
		HashType:  string(defaultHashFunction.Type()),
		LastUsed:  time.Now(),
	}

	// Create the Merkle tree.
	tree, err := arbo.NewTree(arbo.Config{
		Database:     prefixeddb.NewPrefixedDatabase(c.db, censusPrefix(censusID)),
		MaxLevels:    types.CensusTreeMaxLevels,
		HashFunction: defaultHashFunction,
	})
	tree.HashFunction().Type()
	if err != nil {
		return nil, err
	}
	ref.SetTree(tree)
	// Compute and update the current root.
	root, err := tree.Root()
	if err != nil {
		return nil, err
	}
	ref.currentRoot = root

	// Prepare the root update channel.
	ref.updateRootRequest = c.updateRootChan

	// Store the reference in the database.
	if err := c.writeReference(ref); err != nil {
		return nil, err
	}

	// Add to the in‑memory maps.
	c.loadedCensus[censusID] = ref
	rk := rootKey(root)
	if _, exists := c.rootIndex[rk]; !exists {
		c.rootIndex[rk] = censusID
	}

	return ref, nil
}

// writeReference writes a census reference to the database.
func (c *CensusDB) writeReference(ref *CensusRef) error {
	key := append([]byte(censusDBreferencePrefix), ref.ID[:]...)
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(ref); err != nil {
		return err
	}
	wtx := c.db.WriteTx()
	defer wtx.Discard()
	if err := wtx.Set(key, buf.Bytes()); err != nil {
		return err
	}
	return wtx.Commit()
}

// HashAndTrunk computes the hash of a key and truncates it to the required length.
// Returns nil if the hash function fails. Panics if the hash output is too short.
func (c *CensusDB) HashAndTrunkKey(key []byte) []byte {
	length := defaultHashFunction.Len() / 8
	hash, err := defaultHashFunction.Hash(key)
	if err != nil {
		return nil
	}
	if len(hash) < length {
		panic("hash function output is too short, maxlevels is too high")
	}
	return hash[:length]
}

// HashLen returns the length of the hash function output in bytes.
func (c *CensusDB) HashLen() int {
	return defaultHashFunction.Len()
}

// Exists returns true if the censusID exists in the local database.
func (c *CensusDB) Exists(censusID uuid.UUID) bool {
	c.mu.RLock()
	_, exists := c.loadedCensus[censusID]
	c.mu.RUnlock()
	if exists {
		return true
	}
	key := append([]byte(censusDBreferencePrefix), censusID[:]...)
	_, err := c.db.Get(key)
	return err == nil
}

// Load returns a census from memory or from the persistent KV database.
func (c *CensusDB) Load(censusID uuid.UUID) (*CensusRef, error) {
	ref, err := c.loadCensusRef(censusID)
	if err != nil {
		return nil, err
	}
	return ref, nil
}

// loadCensusRef loads a census reference from memory or persistent DB using a double‑check.
func (c *CensusDB) loadCensusRef(censusID uuid.UUID) (*CensusRef, error) {
	c.mu.RLock()
	if ref, exists := c.loadedCensus[censusID]; exists {
		c.mu.RUnlock()
		return ref, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	key := append([]byte(censusDBreferencePrefix), censusID[:]...)
	b, err := c.db.Get(key)
	if err != nil {
		if errors.Is(err, db.ErrKeyNotFound) {
			return nil, fmt.Errorf("%w: %x", ErrCensusNotFound, censusID)
		}
		return nil, err
	}

	var ref CensusRef
	if err := gob.NewDecoder(bytes.NewReader(b)).Decode(&ref); err != nil {
		return nil, err
	}

	tree, err := arbo.NewTree(arbo.Config{
		Database:     prefixeddb.NewPrefixedDatabase(c.db, censusPrefix(censusID)),
		MaxLevels:    ref.MaxLevels,
		HashFunction: defaultHashFunction,
	})
	if err != nil {
		return nil, err
	}
	ref.tree = tree
	root, err := tree.Root()
	if err != nil {
		return nil, err
	}
	ref.currentRoot = root
	ref.updateRootRequest = c.updateRootChan

	// Update the LastUsed timestamp and write back to the database.
	ref.LastUsed = time.Now()
	if err := c.writeReference(&ref); err != nil {
		return nil, err
	}

	c.loadedCensus[censusID] = &ref
	rk := rootKey(root)
	if _, exists := c.rootIndex[rk]; !exists {
		c.rootIndex[rk] = censusID
	}
	return &ref, nil
}

// Del removes a census from the database and memory.
func (c *CensusDB) Del(censusID uuid.UUID) error {
	key := append([]byte(censusDBreferencePrefix), censusID[:]...)
	wtx := c.db.WriteTx()
	if err := wtx.Delete(key); err != nil {
		wtx.Discard()
		return err
	}
	if err := wtx.Commit(); err != nil {
		return err
	}

	c.mu.Lock()
	if ref, exists := c.loadedCensus[censusID]; exists {
		delete(c.rootIndex, rootKey(ref.currentRoot))
		delete(c.loadedCensus, censusID)
	}
	c.mu.Unlock()

	go func(id uuid.UUID) {
		if _, err := deleteCensusTreeFromDatabase(c.db, censusPrefix(id)); err != nil {
			log.Warnw("error deleting census tree", "id", hex.EncodeToString(id[:]), "err", err)
		}
	}(censusID)

	return nil
}

// deleteCensusTreeFromDatabase removes all keys belonging to a census tree from the database.
func deleteCensusTreeFromDatabase(kv db.Database, prefix []byte) (int, error) {
	database := prefixeddb.NewPrefixedDatabase(kv, prefix)
	wtx := database.WriteTx()
	count := 0
	err := database.Iterate(nil, func(k, _ []byte) bool {
		if err := wtx.Delete(k); err != nil {
			log.Warnw("could not remove key from database", "key", hex.EncodeToString(k))
		} else {
			count++
		}
		return true
	})
	if err != nil {
		return 0, err
	}
	return count, wtx.Commit()
}

// ProofByRoot finds a census by its Merkle tree root and generates a Merkle proof for the given leafKey.
// It returns a CensusProof containing the proof components.
func (c *CensusDB) ProofByRoot(root, leafKey []byte) (*types.CensusProof, error) {
	rk := rootKey(root)
	c.mu.RLock()
	censusID, exists := c.rootIndex[rk]
	c.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("no census found with the provided root")
	}
	ref, err := c.Load(censusID)
	if err != nil {
		return nil, err
	}
	key, value, siblings, inclusion, err := ref.GenProof(leafKey)
	if err != nil {
		return nil, err
	}
	if !inclusion {
		return nil, ErrKeyNotFound
	}

	return &types.CensusProof{
		Root:     root,
		Key:      key,
		Value:    value,
		Siblings: siblings,
		Weight:   (*types.BigInt)(arbo.BytesToBigInt(value)),
	}, nil
}

// VerifyProof checks the validity of a Merkle proof.
func (c *CensusDB) VerifyProof(proof *types.CensusProof) bool {
	if proof == nil {
		return false
	}
	// if weight is available, check it
	if proof.Weight != nil {
		if proof.Weight.MathBigInt().Cmp(arbo.BytesToBigInt(proof.Value)) != 0 {
			return false
		}
	}
	return VerifyProof(proof.Key, proof.Value, proof.Root, proof.Siblings)
}

// SizeByRoot returns the number of leaves in the Merkle tree with the given root.
func (c *CensusDB) SizeByRoot(root []byte) (int, error) {
	rk := rootKey(root)
	c.mu.RLock()
	censusID, exists := c.rootIndex[rk]
	c.mu.RUnlock()
	if !exists {
		return 0, fmt.Errorf("no census found with the provided root")
	}
	ref, err := c.Load(censusID)
	if err != nil {
		return 0, err
	}
	return ref.Size(), nil
}

// updateRoot recalculates the Merkle tree root for a given census and updates the in‑memory index.
// It acquires the CensusRef's treeMu before reading or writing currentRoot.
func (c *CensusDB) updateRoot(censusID uuid.UUID, newRoot []byte) error {
	newKey := rootKey(newRoot)
	c.mu.Lock()
	defer c.mu.Unlock()

	ref, exists := c.loadedCensus[censusID]
	if !exists {
		return ErrCensusNotFound
	}

	ref.treeMu.Lock()
	oldKey := rootKey(ref.currentRoot)
	if oldKey == newKey {
		ref.treeMu.Unlock()
		return nil
	}
	ref.currentRoot = append([]byte(nil), newRoot...)
	ref.treeMu.Unlock()

	delete(c.rootIndex, oldKey)
	c.rootIndex[newKey] = censusID
	return nil
}

// censusPrefix returns the prefix used for the census tree in the database.
func censusPrefix(censusID uuid.UUID) []byte {
	return append([]byte(censusDBprefix), censusID[:]...)
}
