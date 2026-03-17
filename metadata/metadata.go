package metadata

import (
	"errors"
	"fmt"

	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

var (
	ErrNotFound = fmt.Errorf("metadata not found")
)

type MetadataStorage struct {
	stg *storage.Storage
}

func New(stg *storage.Storage) *MetadataStorage {
	return &MetadataStorage{
		stg: stg,
	}
}

func (m *MetadataStorage) Get(key types.HexBytes) (*types.Metadata, error) {
	metadata, err := m.stg.Metadata(key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("internal error: %w", err)
	}
	return metadata, nil
}

func (m *MetadataStorage) Set(metadata *types.Metadata) (types.HexBytes, error) {
	return m.stg.SetMetadata(metadata)
}
