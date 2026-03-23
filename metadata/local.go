package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/prefixeddb"
	"github.com/vocdoni/davinci-node/types"
)

var (
	// metadataPrefix is the prefix used to store the metadata in the database
	metadataPrefix = []byte("md/")
	// ErrNotFound is returned when the metadata is not found
	ErrNotFound = fmt.Errorf("metadata not found")
)

// LocalMetadata is a local implementation of the MetadataStorage interface
// that stores the metadata in an local database. It also provides a cache for
// the artifacts.
type LocalMetadata struct {
	db         db.Database
	cache      *lru.Cache[string, *types.Metadata] // Cache for artifacts
	globalLock sync.Mutex
}

// NewLocalMetadata creates a new LocalMetadata instance with the given
// database instance. It also creates a cache for the artifacts if possible
// and returns it.
func NewLocalMetadata(db db.Database) *LocalMetadata {
	cache, _ := lru.New[string, *types.Metadata](1000)
	return &LocalMetadata{
		db:    db,
		cache: cache,
	}
}

// SetMetadata stores the given metadata in the local database and returns an
// error if the request fails.
func (lm *LocalMetadata) SetMetadata(_ context.Context, key types.HexBytes, metadata *types.Metadata) error {
	if metadata == nil {
		return fmt.Errorf("nil metadata")
	}
	lm.globalLock.Lock()
	defer lm.globalLock.Unlock()
	return lm.setValue(metadataPrefix, key, metadata)
}

// Metadata returns the metadata stored in the local database for the given
// key. It returns an error if the request fails.
func (lm *LocalMetadata) Metadata(_ context.Context, key types.HexBytes) (*types.Metadata, error) {
	if key == nil {
		return nil, fmt.Errorf("no key provider")
	}
	// Try to get the metadata from the cache, if the cache is available
	if metadata, ok := lm.cache.Get(string(metadataPrefix) + key.Hex()); ok {
		return metadata, nil
	}
	lm.globalLock.Lock()
	defer lm.globalLock.Unlock()
	// Retrieve the metadata from the storage
	metadata := &types.Metadata{}
	if err := lm.getValue(metadataPrefix, key, metadata); err != nil {
		return nil, err
	}
	// Store the metadata in the cache for future use
	lm.cache.Add(string(metadataPrefix)+key.Hex(), metadata)
	return metadata, nil
}

// setValue stores the given artifact in the local database. It returns an
// error if the request fails.
func (lm *LocalMetadata) setValue(prefix, key types.HexBytes, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("error encoding value: %w", err)
	}

	// instance a write transaction with the prefix provided
	wTx := prefixeddb.NewPrefixedDatabase(lm.db, prefix).WriteTx()
	defer wTx.Discard()

	// store the artifact in the database with the key generated
	if err := wTx.Set(key, data); err != nil {
		return err
	}
	// commit the transaction
	return wTx.Commit()
}

// getValue returns the artifact stored in the local database for the given
// key. It returns an error if the request fails.
func (lm *LocalMetadata) getValue(prefix, key types.HexBytes, v any) error {
	if key == nil {
		return fmt.Errorf("no key provided")
	}

	var data []byte
	var err error
	pdb := prefixeddb.NewPrefixedDatabase(lm.db, prefix)
	data, err = pdb.Get(key)
	if errors.Is(err, db.ErrKeyNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("could not decode artifact: %w", err)
	}

	return nil
}
