package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/prefixeddb"
	"github.com/vocdoni/davinci-node/types"
)

var (
	metadataPrefix = []byte("md/")

	ErrNotFound = fmt.Errorf("metadata not found")
)

type LocalMetadata struct {
	db         db.Database
	cache      *lru.Cache[string, any] // Cache for artifacts
	globalLock sync.Mutex
}

func NewLocalMetadata(db db.Database) *LocalMetadata {
	lm := &LocalMetadata{
		db: db,
	}
	if cache, err := lru.New[string, any](1000); err == nil {
		lm.cache = cache
	}
	return lm
}

func (lm *LocalMetadata) SetMetadata(_ context.Context, key types.HexBytes, metadata *types.Metadata) error {
	if metadata == nil {
		return fmt.Errorf("nil metadata")
	}

	lm.globalLock.Lock()
	defer lm.globalLock.Unlock()

	return lm.setArtifact(metadataPrefix, key, metadata)
}

func (lm *LocalMetadata) Metadata(_ context.Context, key types.HexBytes) (*types.Metadata, error) {
	if key == nil {
		return nil, fmt.Errorf("no key provider")
	}
	// Try to get the metadata from the cache
	val, ok := lm.cache.Get(string(metadataPrefix) + key.Hex())
	if ok {
		if metadata, ok := val.(*types.Metadata); ok {
			return metadata, nil
		}
	}

	lm.globalLock.Lock()
	defer lm.globalLock.Unlock()

	// Retrieve the metadata from the storage
	metadata := &types.Metadata{}
	if err := lm.getArtifact(metadataPrefix, key, metadata); err != nil {
		return nil, err
	}

	// Store the metadata in the cache for future use
	lm.cache.Add(string(metadataPrefix)+key.Hex(), metadata)
	return metadata, nil
}

func (lm *LocalMetadata) setArtifact(prefix, key types.HexBytes, artifact any) error {
	data, err := json.Marshal(artifact)
	if err != nil {
		return fmt.Errorf("error decoding metadata: %w", err)
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

func (lm *LocalMetadata) getArtifact(prefix, key types.HexBytes, out any) error {
	var data []byte
	var err error
	db := prefixeddb.NewPrefixedDatabase(lm.db, prefix)
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

	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("could not decode artifact: %w", err)
	}

	return nil
}
