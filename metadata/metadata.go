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
	// Ensure the metadata key provider is configured
	if ms.keyProvider == nil {
		return nil, fmt.Errorf("metadata key provider is not configured")
	}
	// Iterate over configured providers trying to retrieve the metadata
	getErrors := []error{}
	for _, provider := range ms.providers {
		metadata, err := provider.Metadata(ctx, key)
		if err == nil {
			// If the metadata is found, return it
			return metadata, nil
		}
		// If the error is ErrNotFound, try the next provider
		if errors.Is(err, ErrNotFound) {
			continue
		}
		// For other errors, collect them to be returned/aggregated
		getErrors = append(getErrors, err)
	}
	// If there are non-ErrNotFound errors, return them aggregated
	if len(getErrors) > 0 {
		return nil, fmt.Errorf("failed to get metadata from providers: %w", errors.Join(getErrors...))
	}
	// If the metadata is not found in any provider, return not found error
	return nil, ErrNotFound
}

// Set stores the metadata for the given key in all of the providers. It
// returns the key and an error if any of the providers fails.
func (ms *MetadataStorage) Set(ctx context.Context, metadata *types.Metadata) (types.HexBytes, error) {
	// Ensure the metadata key provider is configured
	if ms.keyProvider == nil {
		return nil, fmt.Errorf("metadata key provider is not configured")
	}
	// Ensure there is at least one metadata provider configured
	if len(ms.providers) == 0 {
		return nil, fmt.Errorf("no metadata providers are configured")
	}
	// Precompute the metadata key
	key, err := ms.keyProvider(metadata)
	if err != nil {
		return nil, err
	}
	// Iterate over configured providers
	setErrors := []error{}
	for _, provider := range ms.providers {
		// Check if the key already exists in the provider
		_, err := provider.Metadata(ctx, key)
		switch {
		case err == nil:
			// Key already exists, skip this provider
			continue
		case errors.Is(err, ErrNotFound):
			// Key not found in this provider, proceed to write
		default:
			// Unexpected error when checking metadata; surface it and skip writing
			setErrors = append(setErrors, fmt.Errorf("metadata read failed: %w", err))
			continue
		}
		// Store the metadata in the provider using the precomputed key
		if err := provider.SetMetadata(ctx, key, metadata); err != nil {
			// If it fails, store the error
			setErrors = append(setErrors, err)
		}
	}
	// If there are some errors, return them
	if len(setErrors) > 0 {
		return key, fmt.Errorf("some providers failed: %w", errors.Join(setErrors...))
	}
	// Otherwise, return the precomputed key
	return key, nil
}
