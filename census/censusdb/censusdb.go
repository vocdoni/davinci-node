package censusdb

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/vocdoni/davinci-node/db"
	leanimt "github.com/vocdoni/lean-imt-go"
	"github.com/vocdoni/lean-imt-go/census"
)

const (
	censusDBprefix           = "cs_"
	censusDBWorkingOnQueries = "cw_" // Prefix for working/temporary censuses during query execution
	censusDBRootPrefix       = "cr_" // Prefix for final censuses identified by their root
	censusDBAddrPrefix       = "ca_" // Prefix for final censuses identified by their Ethereum address

	// CensusKeyMaxLen is the maximum length for census keys (20 bytes for Ethereum addresses)
	CensusKeyMaxLen = 20
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

	// censusHasher is the hash function used for census trees
	censusHasher = leanimt.PoseidonHasher
	// censusHasherName is the name of the hash function used for census trees
	censusHasherName = "poseidon"
	// censusHasherLen is the length of the hash function output in bytes
	censusHasherLen = 32
)

// updateRootRequest is used to update the root of a census tree.
type updateRootRequest struct {
	censusID uuid.UUID
	newRoot  types.HexBytes
	done     chan struct{}
}

// rootKey converts a root (a byte slice) to its canonical hexadecimal string.
func rootKey(root types.HexBytes) string {
	return root.String()
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
				log.Warnw("error updating census root", "id", hex.EncodeToString(req.censusID[:]), "error", err)
			}
			if req.done != nil {
				close(req.done)
			}
		}
	}()

	return c
}

// New creates a new working census with a UUID identifier and adds it to the
// database. It returns ErrCensusAlreadyExists if a census with the given UUID
// is already present.
func (c *CensusDB) New(censusID uuid.UUID) (*CensusRef, error) {
	return c.newCensus(censusID, censusDBWorkingOnQueries, censusID[:], nil)
}

// NewByRoot creates a new census identified by its root. It returns
// ErrCensusAlreadyExists if a census with the given root is already present.
func (c *CensusDB) NewByRoot(root types.HexBytes) (*CensusRef, error) {
	// Generate a deterministic UUID from the root for internal use
	censusID := rootToCensusID(root)
	return c.newCensus(censusID, censusDBRootPrefix, root, nil)
}

// NewByAddress creates a new census identified by an Ethereum address. It
// returns ErrCensusAlreadyExists if a census with the given address is already
// present.
func (c *CensusDB) NewByAddress(address common.Address) (*CensusRef, error) {
	censusID := addressToCensusID(address)
	return c.newCensus(censusID, censusDBAddrPrefix, address.Bytes(), nil)
}

// newCensus is the internal method that creates a new census with the given parameters.
// The tree parameter is optional; if nil, a new empty tree is created.
func (c *CensusDB) newCensus(censusID uuid.UUID, prefix string, keyIdentifier types.HexBytes, tree *census.CensusIMT) (*CensusRef, error) {
	key := append([]byte(prefix), keyIdentifier...)

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
		return nil, fmt.Errorf("error getting db key '%s': %w", string(key), err)
	}

	// Prepare a new census reference.
	ref := &CensusRef{
		ID:                censusID,
		HashType:          censusHasherName,
		LastUsed:          time.Now(),
		updateRootRequest: c.updateRootChan,
	}

	// Create the census tree using leanimt with Pebble persistence and MiMC hasher.
	if tree == nil {
		var err error
		if tree, err = census.NewCensusIMTWithPebble(
			censusPrefix(censusID),
			censusHasher,
		); err != nil {
			return nil, fmt.Errorf("failed to create census tree: %w", err)
		}
	}
	ref.SetTree(tree)

	// Get the current root.
	root, exists := tree.Root()
	if !exists {
		root = big.NewInt(0)
	}
	ref.currentRoot = root.Bytes()

	// Store the reference in the database.
	if err := c.writeReferenceWithPrefix(ref, prefix, keyIdentifier); err != nil {
		return nil, fmt.Errorf("failed to write census reference to db: %w", err)
	}

	// Add to the in‑memory maps.
	c.loadedCensus[censusID] = ref
	rk := rootKey(ref.currentRoot)
	if _, exists := c.rootIndex[rk]; !exists {
		c.rootIndex[rk] = censusID
	}

	return ref, nil
}

// writeReferenceWithPrefix writes a census reference to the database with a specific prefix.
func (c *CensusDB) writeReferenceWithPrefix(ref *CensusRef, prefix string, identifier types.HexBytes) error {
	key := append([]byte(prefix), identifier...)
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

// TrunkKey computes the hash of a key and truncates it to the required length.
// For leanimt census, we use the address directly (20 bytes for Ethereum addresses).
func (c *CensusDB) TrunkKey(key types.HexBytes) types.HexBytes {
	// For lean-imt census, keys are Ethereum addresses (20 bytes)
	if len(key) < 20 {
		// Pad with zeros (right-pad)
		padded := make([]byte, 20)
		copy(padded, key)
		return padded
	} else if len(key) > 20 {
		// Truncate to 20 bytes
		return key[:20]
	}
	return key
}

// HashLen returns the length of the hash function output in bytes.
func (c *CensusDB) HashLen() int {
	return censusHasherLen
}

// Exists returns true if the censusID exists in the local database.
func (c *CensusDB) Exists(censusID uuid.UUID) bool {
	c.mu.RLock()
	_, exists := c.loadedCensus[censusID]
	c.mu.RUnlock()
	if exists {
		return true
	}
	key := censusIDDBPrefix(censusID)
	_, err := c.db.Get(key)
	return err == nil
}

// ExistsByRoot returns true if a census with the given root exists in the local database.
func (c *CensusDB) ExistsByRoot(root types.HexBytes) bool {
	censusID := rootToCensusID(root)
	c.mu.RLock()
	_, exists := c.loadedCensus[censusID]
	c.mu.RUnlock()
	if exists {
		return true
	}
	key := rootDBPrefix(root)
	_, err := c.db.Get(key)
	return err == nil
}

// ExistsByAddress returns true if a census with the given Ethereum address
// exists in the local database.
func (c *CensusDB) ExistsByAddress(address common.Address) bool {
	censusID := addressToCensusID(address)
	c.mu.RLock()
	_, exists := c.loadedCensus[censusID]
	c.mu.RUnlock()
	if exists {
		return true
	}
	key := addressDBPrefix(address)
	_, err := c.db.Get(key)
	return err == nil
}

// Load returns a census from memory or from the persistent KV database.
func (c *CensusDB) Load(censusID uuid.UUID) (*CensusRef, error) {
	return c.loadCensusRef(censusID, censusDBWorkingOnQueries, censusIDDBPrefix(censusID))
}

// LoadByRoot loads a census by its root from memory or from the persistent KV database.
func (c *CensusDB) LoadByRoot(root types.HexBytes) (*CensusRef, error) {
	return c.loadCensusRef(rootToCensusID(root), censusDBRootPrefix, rootDBPrefix(root))
}

func (c *CensusDB) LoadByAddress(address common.Address) (*CensusRef, error) {
	return c.loadCensusRef(addressToCensusID(address), censusDBAddrPrefix, addressDBPrefix(address))
}

// loadCensusRef loads a census reference from memory or persistent DB using a double‑check.
func (c *CensusDB) loadCensusRef(censusID uuid.UUID, dbprefix string, key types.HexBytes) (*CensusRef, error) {
	c.mu.RLock()
	if ref, exists := c.loadedCensus[censusID]; exists {
		c.mu.RUnlock()
		return ref, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

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

	// Reopen the census tree.
	censusTree, err := census.NewCensusIMTWithPebble(
		censusPrefix(censusID),
		censusHasher,
	)
	if err != nil {
		return nil, err
	}
	ref.tree = censusTree
	ref.updateRootRequest = c.updateRootChan

	root, exists := censusTree.Root()
	if !exists {
		root = big.NewInt(0)
	}
	ref.currentRoot = root.Bytes()

	// Update the LastUsed timestamp and write back to the database.
	ref.LastUsed = time.Now()
	if err := c.writeReferenceWithPrefix(&ref, dbprefix, censusID[:]); err != nil {
		return nil, err
	}

	c.loadedCensus[censusID] = &ref
	rk := rootKey(ref.currentRoot)
	if _, exists := c.rootIndex[rk]; !exists {
		c.rootIndex[rk] = censusID
	}
	return &ref, nil
}

// Del removes a census from the database and memory.
func (c *CensusDB) Del(censusID uuid.UUID) error {
	key := append([]byte(censusDBWorkingOnQueries), censusID[:]...)
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
		// Close the tree to release resources.
		if ref.tree != nil {
			_ = ref.tree.Close()
		}
	}
	c.mu.Unlock()

	// Clean up the Pebble directory asynchronously
	go func(id uuid.UUID) {
		path := censusPrefix(id)
		if err := os.RemoveAll(path); err != nil {
			log.Warnw("error deleting census directory", "id", hex.EncodeToString(id[:]), "path", path, "error", err)
		}
	}(censusID)

	return nil
}

// CleanupWorkingCensus removes a working census from the database and memory.
// This is used to clean up temporary censuses after they have been converted to root-based ones.
func (c *CensusDB) CleanupWorkingCensus(censusID uuid.UUID) error {
	startTime := time.Now()

	key := censusIDDBPrefix(censusID)
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
		// Close the tree
		if ref.tree != nil {
			_ = ref.tree.Close()
		}
	}
	c.mu.Unlock()

	// Delete the census tree directory
	path := censusPrefix(censusID)
	err := os.RemoveAll(path)
	duration := time.Since(startTime)

	log.Infow("working census cleanup completed", "censusId", hex.EncodeToString(censusID[:]), "path", path, "duration", duration.String())

	return err
}

// PublishCensus publishes a working census to a root-based census by moving the Pebble directory.
func (c *CensusDB) PublishCensus(originCensusID uuid.UUID, destinationRef *CensusRef) error {
	// Load the working census
	workingRef, err := c.Load(originCensusID)
	if err != nil {
		return err
	}

	// Get root and sync working census (with lock)
	workingRef.treeMu.Lock()
	root, exists := workingRef.tree.Root()
	if !exists {
		workingRef.treeMu.Unlock()
		return fmt.Errorf("working census has no root")
	}

	if err := workingRef.tree.Sync(); err != nil {
		workingRef.treeMu.Unlock()
		return fmt.Errorf("failed to sync working census: %w", err)
	}

	workingPath := censusPrefix(originCensusID)

	// Close working tree
	if err := workingRef.tree.Close(); err != nil {
		workingRef.treeMu.Unlock()
		return fmt.Errorf("failed to close working tree: %w", err)
	}
	workingRef.treeMu.Unlock()

	// Close destination tree (with lock)
	destinationRef.treeMu.Lock()
	destPath := censusPrefix(destinationRef.ID)

	if err := destinationRef.tree.Close(); err != nil {
		destinationRef.treeMu.Unlock()
		return fmt.Errorf("failed to close destination tree: %w", err)
	}
	destinationRef.treeMu.Unlock()

	// Remove destination directory if it exists, then move working directory to destination
	// This is much faster than copying as it's a single rename syscall
	if err := os.RemoveAll(destPath); err != nil {
		return fmt.Errorf("failed to remove destination directory: %w", err)
	}

	if err := os.Rename(workingPath, destPath); err != nil {
		return fmt.Errorf("failed to move census data: %w", err)
	}

	// Reopen destination tree at new location (with lock)
	destinationRef.treeMu.Lock()
	destTree, err := census.NewCensusIMTWithPebble(destPath, censusHasher)
	if err != nil {
		destinationRef.treeMu.Unlock()
		return fmt.Errorf("failed to reopen destination tree: %w", err)
	}
	destinationRef.tree = destTree

	// Verify the root matches
	destRoot, exists := destTree.Root()
	if !exists || destRoot.Cmp(root) != 0 {
		destinationRef.treeMu.Unlock()
		return fmt.Errorf("root mismatch after publish: expected %s, got %s", root.String(), destRoot.String())
	}

	// Update destination ref's current root
	destinationRef.currentRoot = root.Bytes()
	destinationRef.treeMu.Unlock()

	// Clean up working census from memory and metadata DB first
	// (directory already moved, just remove references)
	key := censusIDDBPrefix(originCensusID)
	wtx := c.db.WriteTx()
	if err := wtx.Delete(key); err != nil {
		wtx.Discard()
		return fmt.Errorf("failed to delete working census metadata: %w", err)
	}
	if err := wtx.Commit(); err != nil {
		return fmt.Errorf("failed to commit working census cleanup: %w", err)
	}

	c.mu.Lock()
	// Remove working census from memory (but don't touch root index yet)
	delete(c.loadedCensus, originCensusID)

	// Update the root index to point to destination
	rk := rootKey(root.Bytes())
	c.rootIndex[rk] = destinationRef.ID
	c.mu.Unlock()

	log.Infow("successfully published census by moving directory",
		"originCensusId", hex.EncodeToString(originCensusID[:]),
		"destinationCensusId", hex.EncodeToString(destinationRef.ID[:]),
		"root", hex.EncodeToString(root.Bytes()))

	return nil
}

// VerifyProof checks the validity of a Merkle proof.
func (c *CensusDB) VerifyProof(proof *types.CensusProof) bool {
	if proof == nil {
		return false
	}
	// If weight is available, check it matches the value
	if proof.Weight != nil {
		// For leanimt census, the value is the packed address+weight.
		// Reconstruct: packed = (address << 88) | weight
		addr := common.BytesToAddress(proof.Address)
		packedValue := new(big.Int).Lsh(addr.Big(), 88)
		packedValue.Or(packedValue, proof.Weight.MathBigInt())
		if packedValue.Cmp(new(big.Int).SetBytes(proof.Value)) != 0 {
			return false
		}
	}

	// Verify using leanimt.
	siblings := unpackSiblings(proof.Siblings)
	merkleProof := leanimt.MerkleProof[*big.Int]{
		Root:     new(big.Int).SetBytes(proof.Root),
		Leaf:     new(big.Int).SetBytes(proof.Value),
		Index:    proof.Index,
		Siblings: siblings,
	}
	return leanimt.VerifyProofWith(merkleProof, censusHasher, leanimt.BigIntEqual)
}

// ProofByRoot generates a Merkle proof for the given leafKey in a census identified by its root.
func (c *CensusDB) ProofByRoot(root, leafKey types.HexBytes) (*types.CensusProof, error) {
	// Load census by root directly
	ref, err := c.LoadByRoot(root)
	if err != nil {
		return nil, fmt.Errorf("no census found with the provided root: %w", err)
	}

	// Convert leafKey to Ethereum address.
	if len(leafKey) != 20 {
		return nil, fmt.Errorf("invalid key length: expected 20 bytes, got %d", len(leafKey))
	}
	addr := common.BytesToAddress(leafKey)

	// Generate proof using leanimt census.
	proof, err := ref.tree.GenerateProof(addr)
	if err != nil {
		return nil, err
	}

	// Convert leanimt proof to CensusProof.
	// Reconstruct packed value: packed = (address << 88) | weight
	packedValue := new(big.Int).Lsh(new(big.Int).SetBytes(addr.Bytes()), 88)
	packedValue.Or(packedValue, proof.Weight)

	return &types.CensusProof{
		Root:     proof.Root.Bytes(),
		Address:  addr.Bytes(),
		Value:    packedValue.Bytes(),
		Siblings: packSiblings(proof.Siblings),
		Weight:   (*types.BigInt)(proof.Weight),
		Index:    proof.Index,
	}, nil
}

// SizeByRoot returns the number of leaves in the Merkle tree with the given root.
func (c *CensusDB) SizeByRoot(root types.HexBytes) (int, error) {
	// Load census by root directly
	ref, err := c.LoadByRoot(root)
	if err != nil {
		return 0, fmt.Errorf("no census found with the provided root: %w", err)
	}
	return ref.Size(), nil
}

// PurgeWorkingCensuses removes all working censuses older than the specified duration.
func (c *CensusDB) PurgeWorkingCensuses(maxAge time.Duration) (int, error) {
	cutoffTime := time.Now().Add(-maxAge)
	var keysToDelete [][]byte
	var censusIDsToDelete []uuid.UUID

	err := c.db.Iterate([]byte(censusDBWorkingOnQueries), func(key, value []byte) bool {
		var ref CensusRef
		if err := gob.NewDecoder(bytes.NewReader(value)).Decode(&ref); err != nil {
			return true // Continue iteration
		}

		// Delete censuses that are older than the cutoff time
		if ref.LastUsed.Before(cutoffTime) {
			keysToDelete = append(keysToDelete, censusIDDBPrefix(ref.ID))
			censusIDsToDelete = append(censusIDsToDelete, ref.ID)
		}
		return true
	})
	if err != nil {
		return 0, fmt.Errorf("failed to iterate for purge: %w", err)
	}

	if len(keysToDelete) == 0 {
		return 0, nil
	}

	// Delete in transaction
	wtx := c.db.WriteTx()
	defer func() {
		if wtx != nil {
			wtx.Discard()
		}
	}()

	for _, key := range keysToDelete {
		if err := wtx.Delete(key); err != nil {
			return 0, fmt.Errorf("failed to delete working census: %w", err)
		}
	}

	if err := wtx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit purge: %w", err)
	}
	wtx = nil // Prevent discard from being called

	// Remove from memory and clean up tree data
	c.mu.Lock()
	for _, censusID := range censusIDsToDelete {
		if ref, exists := c.loadedCensus[censusID]; exists {
			delete(c.rootIndex, rootKey(ref.currentRoot))
			delete(c.loadedCensus, censusID)
			if ref.tree != nil {
				_ = ref.tree.Close()
			}
		}
	}
	c.mu.Unlock()

	// Clean up tree directories asynchronously
	for _, censusID := range censusIDsToDelete {
		go func(id uuid.UUID) {
			path := censusPrefix(id)
			if err := os.RemoveAll(path); err != nil {
				log.Warnw("error deleting purged census directory",
					"id", hex.EncodeToString(id[:]),
					"path", path,
					"error", err)
			}
		}(censusID)
	}

	log.Infow("purged old working censuses",
		"purgedCount", len(keysToDelete),
		"maxAge", maxAge.String())

	return len(keysToDelete), nil
}

// updateRoot recalculates the Merkle tree root for a given census and updates the in‑memory index.
// It acquires the CensusRef's treeMu before reading or writing currentRoot.
func (c *CensusDB) updateRoot(censusID uuid.UUID, newRoot types.HexBytes) error {
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
	ref.currentRoot = newRoot.Bytes()
	ref.treeMu.Unlock()

	delete(c.rootIndex, oldKey)
	c.rootIndex[newKey] = censusID
	return nil
}

// censusPrefix returns the directory path used for the census tree in the database.
func censusPrefix(censusID uuid.UUID) string {
	tmpDir := os.TempDir()
	return filepath.Join(tmpDir, fmt.Sprintf("%s%x", censusDBprefix, censusID[:]))
}

// rootToCensusID generates a deterministic UUID from the given root. It uses
// SHA-1 hashing and ensures the root is left-trimmed of leading zeros before
// hashing.
func rootToCensusID(root types.HexBytes) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, root.LeftTrim())
}

// addressToCensusID generates a deterministic UUID from the given address. It
// uses SHA-1 hashing.
func addressToCensusID(address common.Address) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, address.Bytes())
}

// censusIDDBPrefix generates the database key prefix for a census identified
// by its UUID.
func censusIDDBPrefix(censusID uuid.UUID) []byte {
	return append([]byte(censusDBWorkingOnQueries), censusID[:]...)
}

// censusDBRootPrefix generates the database key prefix for a census identified
// by its root. It ensures the root is left-trimmed of leading zeros before
// appending to the prefix.
func rootDBPrefix(root types.HexBytes) []byte {
	return append([]byte(censusDBRootPrefix), root.LeftTrim()...)
}

func addressDBPrefix(address common.Address) []byte {
	return append([]byte(censusDBAddrPrefix), address.Bytes()...)
}

// packSiblings packs a slice of big.Int siblings into a byte array.
// Each sibling is encoded as 32 bytes in big-endian format.
func packSiblings(siblings []*big.Int) []byte {
	if len(siblings) == 0 {
		return []byte{}
	}
	packed := make([]byte, 0, len(siblings)*32)
	for _, s := range siblings {
		siblingBytes := make([]byte, 32)
		s.FillBytes(siblingBytes)
		packed = append(packed, siblingBytes...)
	}
	return packed
}

// unpackSiblings unpacks a byte array into a slice of big.Int siblings.
// Each sibling is expected to be 32 bytes in big-endian format.
func unpackSiblings(packed []byte) []*big.Int {
	if len(packed) == 0 {
		return []*big.Int{}
	}
	numSiblings := len(packed) / 32
	siblings := make([]*big.Int, numSiblings)
	for i := 0; i < numSiblings; i++ {
		siblings[i] = new(big.Int).SetBytes(packed[i*32 : (i+1)*32])
	}
	return siblings
}

// BigIntSiblings converts packed siblings bytes to a slice of big.Int siblings.
// This is a public wrapper for unpackSiblings, provided for compatibility with
// existing code that expects big.Int siblings.
func BigIntSiblings(siblings []byte) ([]*big.Int, error) {
	return unpackSiblings(siblings), nil
}

// Import imports a census from a JSON-encoded census dump read from an
// io.Reader. It creates a new census tree, populates it with the data from the
// dump, and creates a new CensusRef with the imported tree. It returns the
// CensusRef and any error encountered during the process, such as decoding
// errors or tree creation/import errors.
func (c *CensusDB) Import(root types.HexBytes, reader io.Reader) (*CensusRef, error) {
	// Create a new census tree by its root
	censusID := rootToCensusID(root.Bytes())
	tree, err := census.NewCensusIMT(c.db, censusHasher)
	if err != nil {
		return nil, fmt.Errorf("failed to create census tree: %w", err)
	}
	// Import the dump into the tree
	if err := tree.Import(root.BigInt().MathBigInt(), reader); err != nil {
		return nil, fmt.Errorf("failed to import census dump into tree: %w", err)
	}
	if err := tree.Sync(); err != nil {
		return nil, fmt.Errorf("failed to sync census tree after import: %w", err)
	}
	// Create a new CensusRef with the imported tree
	return c.newCensus(censusID, censusDBRootPrefix, root.Bytes(), tree)
}

// ImportAll imports a census from a JSON-encoded census dump. It decodes the
// dump, creates a new census tree, and populates it with the data from the
// dump. Then, it creates a new CensusRef with the imported tree and adds it
// to the database. It returns the CensusRef and any error encountered during
// the process, such as decoding errors or tree creation/import errors.
func (c *CensusDB) ImportAll(data []byte) (*CensusRef, error) {
	// Decode the census dump from JSON
	var dump census.CensusDump
	if err := json.Unmarshal(data, &dump); err != nil {
		return nil, fmt.Errorf("failed to unmarshal census dump: %w", err)
	}
	// Create a new census tree by its root
	censusID := rootToCensusID(dump.Root.Bytes())
	tree, err := census.NewCensusIMTWithPebble(
		censusPrefix(censusID),
		censusHasher,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create census tree: %w", err)
	}
	// Import the dump into the tree
	if err := tree.ImportAll(&dump); err != nil {
		return nil, fmt.Errorf("failed to import census dump into tree: %w", err)
	}
	// Create a new CensusRef with the imported tree
	return c.newCensus(censusID, censusDBRootPrefix, dump.Root.Bytes(), tree)
}

// ImportEvents imports a census from a list of census events. It creates a
// new census tree by the root provided, applies the events checking against
// the that root, and returns the CensusRef.
func (c *CensusDB) ImportEvents(root types.HexBytes, events []census.CensusEvent) (*CensusRef, error) {
	// Create a new census tree by its root
	ref, err := c.NewByRoot(root)
	if err != nil {
		return nil, fmt.Errorf("failed to create census tree: %w", err)
	}

	// Import the events into the tree
	if err := ref.ApplyEvents(events); err != nil {
		return nil, fmt.Errorf("failed to apply census events: %w", err)
	}

	// Check that the final root matches the expected root
	if finalRoot := ref.Root(); !finalRoot.Equal(root) {
		return nil, fmt.Errorf("final root mismatch after applying events: expected %s, got %s",
			root.String(),
			finalRoot.String())
	}
	return ref, nil
}

// ImportEventsByAddress imports a census from a list of census events,
// identified by an Ethereum address. It creates a new census tree by the
// address provided, applies the events checking against the expected root,
// and returns the CensusRef.
func (c *CensusDB) ImportEventsByAddress(
	address common.Address,
	expectedRoot types.HexBytes,
	events []census.CensusEvent,
) (*CensusRef, error) {
	// Create a new census tree by its address
	ref, err := c.NewByAddress(address)
	if err != nil {
		return nil, fmt.Errorf("failed to create census tree: %w", err)
	}

	// Import the events into the tree
	if err := ref.ApplyEvents(events); err != nil {
		return nil, fmt.Errorf("failed to apply census events: %w", err)
	}

	// Check that the final root matches the expected root
	if finalRoot := ref.Root(); !finalRoot.Equal(expectedRoot) {
		return nil, fmt.Errorf("final root mismatch after applying events: expected %s, got %s",
			expectedRoot.String(),
			finalRoot.String())
	}
	return ref, nil
}
