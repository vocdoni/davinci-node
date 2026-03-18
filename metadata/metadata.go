package metadata

import (
	"context"
	"errors"
	"fmt"

	"github.com/vocdoni/davinci-node/types"
)

type MetadataProvider interface {
	SetMetadata(ctx context.Context, key types.HexBytes, metadata *types.Metadata) error
	Metadata(ctx context.Context, key types.HexBytes) (*types.Metadata, error)
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

func (ms *MetadataStorage) Get(ctx context.Context, key types.HexBytes) (*types.Metadata, error) {
	for _, provider := range ms.providers {
		metadata, err := provider.Metadata(ctx, key)
		if err == nil {
			return metadata, nil
		}
	}
	return nil, fmt.Errorf("metadata not found")
}

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
