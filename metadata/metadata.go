package metadata

import (
	"context"
	"errors"
	"fmt"

	"github.com/vocdoni/davinci-node/types"
)

// MetadataKeyProvider is a function that returns the key for a given metadata
type MetadataKeyProvider func(data any) (types.HexBytes, error)

// MetadataProvider is an interface for storing and retrieving metadata
type MetadataProvider interface {
	SetMetadata(ctx context.Context, key types.HexBytes, metadata *types.Metadata) error
	Metadata(ctx context.Context, key types.HexBytes) (*types.Metadata, error)
}

// MetadataStorage is a struct that stores and retrieves metadata for a given
// key. It wraps multiple metadata providers storing the metadata in all of
// them but retrieving it from the first one that has it.
type MetadataStorage struct {
	keyProvider MetadataKeyProvider
	providers   []MetadataProvider
}

// New returns a new MetadataStorage for the given MetadataKeyProvider and
// MetadataProviders.
func New(keyProvider MetadataKeyProvider, providers ...MetadataProvider) *MetadataStorage {
	return &MetadataStorage{
		keyProvider: keyProvider,
		providers:   providers,
	}
}

// Get returns the metadata for the given key from the first provider that has
// it or an error if none of the providers has it.
func (ms *MetadataStorage) Get(ctx context.Context, key types.HexBytes) (*types.Metadata, error) {
	for _, provider := range ms.providers {
		metadata, err := provider.Metadata(ctx, key)
		if err == nil {
			return metadata, nil
		}
	}
	return nil, fmt.Errorf("metadata not found")
}

// Set stores the metadata for the given key in all of the providers. It
// returns the key and an error if any of the providers fails.
func (ms *MetadataStorage) Set(ctx context.Context, metadata *types.Metadata) (types.HexBytes, error) {
	key, err := ms.keyProvider(metadata)
	if err != nil {
		return nil, err
	}
	setErrors := []error{}
	for _, provider := range ms.providers {
		if _, err := provider.Metadata(ctx, key); err == nil {
			continue
		}
		if err := provider.SetMetadata(ctx, key, metadata); err != nil {
			setErrors = append(setErrors, err)
		}
	}
	if len(setErrors) > 0 {
		return key, fmt.Errorf("some providers failed: %w", errors.Join(setErrors...))
	}
	return key, nil
}
