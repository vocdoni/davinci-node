package metadata

import (
	"errors"
	"fmt"

	"github.com/vocdoni/davinci-node/types"
)

type MetadataProvider interface {
	SetMetadata(key types.HexBytes, metadata *types.Metadata) error
	Metadata(key types.HexBytes) (*types.Metadata, error)
}

type MetadataKeyProvider func(data any) (types.HexBytes, error)

type MetadataStorage struct {
	keyProvider MetadataKeyProvider
	providers   []MetadataProvider
}

func New(keyProvider MetadataKeyProvider, providers ...MetadataProvider) *MetadataStorage {
	return &MetadataStorage{
		keyProvider: keyProvider,
		providers:   providers,
	}
}

func (ms *MetadataStorage) Get(key types.HexBytes) (*types.Metadata, error) {
	for _, provider := range ms.providers {
		metadata, err := provider.Metadata(key)
		if err == nil {
			return metadata, nil
		}
	}
	return nil, fmt.Errorf("metadata not found")
}

func (ms *MetadataStorage) Set(metadata *types.Metadata) (types.HexBytes, error) {
	key, err := ms.keyProvider(metadata)
	if err != nil {
		return nil, err
	}
	setErrors := []error{}
	for _, provider := range ms.providers {
		if err := provider.SetMetadata(key, metadata); err != nil {
			setErrors = append(setErrors, err)
		}
	}
	if len(setErrors) != len(ms.providers) {
		return key, fmt.Errorf("some providers failed: %w", errors.Join(setErrors...))
	}
	return key, nil
}
