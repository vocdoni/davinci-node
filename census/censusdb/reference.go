package censusdb

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/vocdoni/davinci-node/types"
	leanimt "github.com/vocdoni/lean-imt-go"
	"github.com/vocdoni/lean-imt-go/census"
)

// CensusRef is a reference to a census. It holds the Merkle tree.
// All accesses to the underlying tree (and its currentRoot) are protected by treeMu.
type CensusRef struct {
	ID          uuid.UUID
	HashType    string
	LastUsed    time.Time
	currentRoot []byte
	tree        *census.CensusIMT `gob:"-"`
	// treeMu protects all access to the underlying Merkle tree.
	treeMu sync.Mutex `gob:"-"`
	// updateRootRequest channel for async root updates
	updateRootRequest chan<- *updateRootRequest `gob:"-"`
}

// Tree returns the underlying census.CensusIMT pointer.
// (Not concurrency‑safe; use Insert, Root, or GenProof.)
func (cr *CensusRef) Tree() *census.CensusIMT {
	return cr.tree
}

// SetTree sets the census.CensusIMT pointer.
func (cr *CensusRef) SetTree(tree *census.CensusIMT) {
	cr.tree = tree
}

// Insert safely inserts a key/value pair into the Merkle tree.
// It holds treeMu during the Add and Root calls.
// Key must be exactly 20 bytes (Ethereum address).
func (cr *CensusRef) Insert(key, value types.HexBytes) error {
	cr.treeMu.Lock()
	defer cr.treeMu.Unlock()

	// Convert key to 20-byte Ethereum address
	if len(key) != 20 {
		return fmt.Errorf("key must be exactly 20 bytes (Ethereum address length), got %d", len(key))
	}
	addr := common.BytesToAddress(key)
	weight := value.BigInt().MathBigInt()

	// Add to census (handles address-weight packing internally)
	err := cr.tree.Add(addr, weight)
	if err != nil {
		return err
	}

	// Update the current root
	root, exists := cr.tree.Root()
	if exists {
		cr.currentRoot = root.Bytes()
		// Send async root update request if channel is available
		if cr.updateRootRequest != nil {
			select {
			case cr.updateRootRequest <- &updateRootRequest{
				censusID: cr.ID,
				newRoot:  cr.currentRoot,
			}:
			default:
				// Channel full, skip update (will be updated on next operation)
			}
		}
	}

	return nil
}

// InsertBatch safely inserts a batch of key/value pairs into the Merkle tree.
func (cr *CensusRef) InsertBatch(keys, values []types.HexBytes) ([]interface{}, error) {
	cr.treeMu.Lock()
	defer cr.treeMu.Unlock()

	if len(keys) != len(values) {
		return nil, fmt.Errorf("keys and values must have same length: %d != %d", len(keys), len(values))
	}

	// Convert all keys to addresses, values to big.Int weights
	addresses := make([]common.Address, len(keys))
	weights := make([]*big.Int, len(values))

	for i, key := range keys {
		if len(key) != 20 {
			return nil, fmt.Errorf("key %d must be 20 bytes, got %d", i, len(key))
		}
		addresses[i] = common.BytesToAddress(key)
		weights[i] = values[i].BigInt().MathBigInt()
	}

	// Add bulk
	err := cr.tree.AddBulk(addresses, weights)
	if err != nil {
		return nil, err
	}

	// Update the current root
	root, exists := cr.tree.Root()
	if exists {
		cr.currentRoot = root.Bytes()
		// Send async root update request if channel is available
		if cr.updateRootRequest != nil {
			select {
			case cr.updateRootRequest <- &updateRootRequest{
				censusID: cr.ID,
				newRoot:  cr.currentRoot,
			}:
			default:
				// Channel full, skip update
			}
		}
	}

	return nil, nil
}

// FetchKeysAndValues fetches all keys and values from the Merkle tree.
// Returns the keys as byte arrays (20-byte addresses) and the values as BigInts (weights).
// Note: This is a placeholder implementation. The lean-imt census package doesn't expose
// a direct iteration API, so this would need to be implemented if required.
func (cr *CensusRef) FetchKeysAndValues() ([]types.HexBytes, []*types.BigInt, error) {
	cr.treeMu.Lock()
	defer cr.treeMu.Unlock()

	// TODO: Implement this if needed. Options:
	// 1. Add iteration support to lean-imt-go/census package
	// 2. Maintain a separate index of addresses
	// 3. Use the underlying LeanIMT's iteration if exposed

	return nil, nil, fmt.Errorf("FetchKeysAndValues not yet implemented for lean-imt census")
}

// Root safely returns the current Merkle tree root.
func (cr *CensusRef) Root() types.HexBytes {
	cr.treeMu.Lock()
	defer cr.treeMu.Unlock()
	root, exists := cr.tree.Root()
	if exists {
		return root.Bytes()
	}
	return nil
}

// Size safely returns the number of leaves in the Merkle tree.
func (cr *CensusRef) Size() int {
	cr.treeMu.Lock()
	defer cr.treeMu.Unlock()
	return cr.tree.Size()
}

// GenProof safely generates a Merkle proof for the given leaf key.
// It returns the proof components (key, value, siblings, index) and an inclusion boolean.
// For lean-imt, key must be a 20-byte Ethereum address.
func (cr *CensusRef) GenProof(key types.HexBytes) (types.HexBytes, types.HexBytes, types.HexBytes, uint64, bool, error) {
	cr.treeMu.Lock()
	defer cr.treeMu.Unlock()

	if len(key) != 20 {
		return nil, nil, nil, 0, false, fmt.Errorf("key must be 20 bytes")
	}
	addr := common.BytesToAddress(key)

	proof, err := cr.tree.GenerateProof(addr)
	if err != nil {
		return nil, nil, nil, 0, false, err
	}

	// Reconstruct packed value from address and weight: packed = (address << 88) | weight
	packedValue := new(big.Int).Lsh(new(big.Int).SetBytes(proof.Address.Bytes()), 88)
	packedValue.Or(packedValue, proof.Weight)

	siblings := packSiblings(proof.Siblings)

	return key, packedValue.Bytes(), siblings, proof.Index, true, nil
}

// ApplyEvents safely applies a list of census events to the Merkle tree.
func (cr *CensusRef) ApplyEvents(root types.HexBytes, events []census.CensusEvent) error {
	cr.treeMu.Lock()
	defer cr.treeMu.Unlock()

	return cr.tree.ApplyEvents(root.BigInt().MathBigInt(), events)
}

// VerifyProof verifies a Merkle proof for the given leaf key.
// Uses lean-imt verification with the configured hash function.
func VerifyProof(key, value, root, siblings types.HexBytes, index uint64) bool {
	// Unpack siblings from bytes to []*big.Int
	siblingsUnpacked := unpackSiblings(siblings)

	// Create MerkleProof structure
	merkleProof := leanimt.MerkleProof[*big.Int]{
		Root:     root.BigInt().MathBigInt(),
		Leaf:     value.BigInt().MathBigInt(),
		Index:    index,
		Siblings: siblingsUnpacked,
	}

	// Verify using the configured hasher
	return leanimt.VerifyProofWith(merkleProof, censusHasher, leanimt.BigIntEqual)
}
