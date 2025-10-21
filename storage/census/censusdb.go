package census

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	leanimt "github.com/vocdoni/lean-imt-go"
	"github.com/vocdoni/lean-imt-go/census"

	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
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
		ID:       censusID,
		HashType: "poseidon",
		LastUsed: time.Now(),
	}

	// Create the census tree using leanimt with Pebble persistence.
	censusTree, err := census.NewCensusIMTWithPebble(censusPrefix(censusID), leanimt.PoseidonHasher)
	if err != nil {
		return nil, err
	}
	ref.SetTree(censusTree)

	// Get the current root.
	root, exists := censusTree.Root()
	if !exists {
		root = big.NewInt(0)
	}
	ref.currentRoot = root.Bytes()

	// Prepare the root update channel.
	ref.updateRootRequest = c.updateRootChan

	// Store the reference in the database.
	if err := c.writeReference(ref); err != nil {
		return nil, err
	}

	// Add to the in‑memory maps.
	c.loadedCensus[censusID] = ref
	rk := rootKey(ref.currentRoot)
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

// HashAndTrunkKey computes the hash of a key and truncates it to the required length.
// For leanimt, we use the address directly (20 bytes for Ethereum addresses).
func (c *CensusDB) HashAndTrunkKey(key []byte) []byte {
	// For leanimt census, keys are Ethereum addresses (20 bytes)
	if len(key) > 20 {
		return key[:20]
	}
	return key
}

// HashLen returns the length of the hash function output in bytes.
// Poseidon hash outputs 32 bytes.
func (c *CensusDB) HashLen() int {
	return 32
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

	// Reopen the census tree.
	censusTree, err := census.NewCensusIMTWithPebble(censusPrefix(censusID), leanimt.PoseidonHasher)
	if err != nil {
		return nil, err
	}
	ref.tree = censusTree

	root, exists := censusTree.Root()
	if !exists {
		root = big.NewInt(0)
	}
	ref.currentRoot = root.Bytes()
	ref.updateRootRequest = c.updateRootChan

	// Update the LastUsed timestamp and write back to the database.
	ref.LastUsed = time.Now()
	if err := c.writeReference(&ref); err != nil {
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
		// Close the tree to release resources.
		if ref.tree != nil {
			_ = ref.tree.Close()
		}
	}
	c.mu.Unlock()

	// Note: leanimt's Pebble database is stored in a directory.
	// We should clean up the directory asynchronously.
	go func(id uuid.UUID) {
		// The census data is stored in the directory specified by censusPrefix.
		// For now, we'll leave the cleanup to the OS or manual intervention.
		log.Infow("census deleted, manual cleanup of directory may be needed",
			"id", hex.EncodeToString(id[:]),
			"path", censusPrefix(id))
	}(censusID)

	return nil
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

	// Convert leafKey to Ethereum address.
	if len(leafKey) != 20 {
		return nil, fmt.Errorf("invalid key length: expected 20 bytes, got %d", len(leafKey))
	}
	addr := common.BytesToAddress(leafKey)

	// Generate proof using leanimt census.
	proof, err := ref.GenProof(addr)
	if err != nil {
		return nil, err
	}

	// Convert leanimt proof to types.CensusProof.
	// The census proof already contains the packed address+weight value internally.
	// We need to reconstruct it: packed = (address << 88) | weight
	packedValue := new(big.Int).Lsh(new(big.Int).SetBytes(proof.Address.Bytes()), 88)
	packedValue.Or(packedValue, proof.Weight)

	return &types.CensusProof{
		CensusOrigin: types.CensusOriginMerkleTree,
		Root:         proof.Root.Bytes(),
		Address:      addr.Bytes(),
		Value:        packedValue.Bytes(),
		Siblings:     packSiblings(proof.Siblings),
		Weight:       (*types.BigInt)(proof.Weight),
		Index:        proof.Index,
	}, nil
}

// VerifyProof checks the validity of a Merkle proof.
func (c *CensusDB) VerifyProof(proof *types.CensusProof) bool {
	if proof == nil {
		return false
	}
	// If weight is available, check it matches the value.
	if proof.Weight != nil {
		// For leanimt census, the value is the packed address+weight.
		// Reconstruct: packed = (address << 88) | weight
		addr := common.BytesToAddress(proof.Address)
		packedValue := new(big.Int).Lsh(new(big.Int).SetBytes(addr.Bytes()), 88)
		packedValue.Or(packedValue, proof.Weight.MathBigInt())
		if packedValue.Cmp(new(big.Int).SetBytes(proof.Value)) != 0 {
			return false
		}
	}

	// Verify using leanimt.
	// Create a MerkleProof structure for verification.
	siblings := unpackSiblings(proof.Siblings)
	merkleProof := leanimt.MerkleProof[*big.Int]{
		Root:     new(big.Int).SetBytes(proof.Root),
		Leaf:     new(big.Int).SetBytes(proof.Value),
		Index:    proof.Index,
		Siblings: siblings,
	}
	return leanimt.VerifyProofWith(merkleProof, leanimt.PoseidonHasher, leanimt.BigIntEqual)
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

// censusPrefix returns the directory path used for the census tree in the database.
// If TMPDIR environment variable is set, it uses that, otherwise uses /tmp.
func censusPrefix(censusID uuid.UUID) string {
	tmpDir := os.TempDir()
	return filepath.Join(tmpDir, fmt.Sprintf("%s%x", censusDBprefix, censusID[:]))
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
