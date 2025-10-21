package census

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/vocdoni/lean-imt-go/census"

	"github.com/vocdoni/davinci-node/types"
)

// CensusRef is a reference to a census. It holds the leanimt census tree.
// All accesses to the underlying tree (and its currentRoot) are protected by treeMu.
type CensusRef struct {
	ID          uuid.UUID
	HashType    string
	LastUsed    time.Time
	currentRoot []byte
	tree        *census.CensusIMT `gob:"-"`
	// treeMu protects all access to the underlying census tree.
	treeMu sync.Mutex `gob:"-"`
	// updateRootRequest is the channel to send asynchronous root update requests.
	updateRootRequest chan *updateRootRequest `gob:"-"`
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

// sendUpdateRoot sends an update request over the channel and waits until processed.
func (cr *CensusRef) sendUpdateRoot(newRoot []byte) error {
	done := make(chan struct{})
	req := &updateRootRequest{
		censusID: cr.ID,
		newRoot:  newRoot,
		done:     done,
	}
	cr.updateRootRequest <- req
	<-done
	return nil
}

// Insert safely inserts an address/weight pair into the census tree.
// It holds treeMu during the Add and Root calls.
func (cr *CensusRef) Insert(key, value []byte) error {
	// Convert key to Ethereum address (20 bytes).
	if len(key) != 20 {
		return fmt.Errorf("invalid key length: expected 20 bytes, got %d", len(key))
	}
	addr := common.BytesToAddress(key)

	// Convert value to weight (big.Int).
	weight := new(big.Int).SetBytes(value)

	cr.treeMu.Lock()
	err := cr.tree.Add(addr, weight)
	if err != nil {
		cr.treeMu.Unlock()
		return err
	}
	root, exists := cr.tree.Root()
	if !exists {
		root = big.NewInt(0)
	}
	newRoot := root.Bytes()
	cr.treeMu.Unlock()

	return cr.sendUpdateRoot(newRoot)
}

// InsertBatch safely inserts a batch of address/weight pairs into the census tree.
func (cr *CensusRef) InsertBatch(keys, values [][]byte) ([]interface{}, error) {
	if len(keys) != len(values) {
		return nil, fmt.Errorf("keys and values length mismatch")
	}

	// Convert keys to addresses and values to weights.
	addresses := make([]common.Address, len(keys))
	weights := make([]*big.Int, len(values))
	for i := range keys {
		if len(keys[i]) != 20 {
			return nil, fmt.Errorf("invalid key length at index %d: expected 20 bytes, got %d", i, len(keys[i]))
		}
		addresses[i] = common.BytesToAddress(keys[i])
		weights[i] = new(big.Int).SetBytes(values[i])
	}

	cr.treeMu.Lock()
	err := cr.tree.AddBulk(addresses, weights)
	if err != nil {
		cr.treeMu.Unlock()
		return nil, err
	}
	root, exists := cr.tree.Root()
	if !exists {
		root = big.NewInt(0)
	}
	newRoot := root.Bytes()
	cr.treeMu.Unlock()

	// leanimt doesn't return invalid entries like arbo does.
	// Return empty slice for compatibility.
	return []interface{}{}, cr.sendUpdateRoot(newRoot)
}

// FetchKeysAndValues fetches all keys and values from the census tree.
// Returns the keys as byte arrays (addresses) and the values as BigInts (weights).
// NOTE: This is not efficiently supported by leanimt. The census tree doesn't
// expose an iteration API. This method returns an error for now.
// TODO: Consider maintaining a separate index of addresses if this functionality is needed.
func (cr *CensusRef) FetchKeysAndValues() ([]types.HexBytes, []*types.BigInt, error) {
	return nil, nil, fmt.Errorf("FetchKeysAndValues is not supported by leanimt census implementation")
}

// Root safely returns the current census tree root.
func (cr *CensusRef) Root() []byte {
	cr.treeMu.Lock()
	defer cr.treeMu.Unlock()
	root, exists := cr.tree.Root()
	if !exists {
		return big.NewInt(0).Bytes()
	}
	return root.Bytes()
}

// Size safely returns the number of leaves in the census tree.
func (cr *CensusRef) Size() int {
	cr.treeMu.Lock()
	defer cr.treeMu.Unlock()
	return cr.tree.Size()
}

// GenProof safely generates a census proof for the given address.
// It returns a census.CensusProof structure.
func (cr *CensusRef) GenProof(addr common.Address) (*census.CensusProof, error) {
	cr.treeMu.Lock()
	defer cr.treeMu.Unlock()
	return cr.tree.GenerateProof(addr)
}

// BigIntSiblings converts packed siblings bytes to a slice of big.Int siblings.
// This is for compatibility with existing code that expects big.Int siblings.
func BigIntSiblings(siblings []byte) ([]*big.Int, error) {
	return unpackSiblings(siblings), nil
}
