package metadata

import (
	"context"
	"fmt"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
)

type fakeMetadataProvider struct {
	metadataFn    func(context.Context, types.HexBytes) (*types.Metadata, error)
	setMetadataFn func(context.Context, types.HexBytes, *types.Metadata) error
	metadataCalls int
	setCalls      int
}

func (f *fakeMetadataProvider) Metadata(ctx context.Context, key types.HexBytes) (*types.Metadata, error) {
	f.metadataCalls++
	if f.metadataFn != nil {
		return f.metadataFn(ctx, key)
	}
	return nil, ErrNotFound
}

func (f *fakeMetadataProvider) SetMetadata(ctx context.Context, key types.HexBytes, metadata *types.Metadata) error {
	f.setCalls++
	if f.setMetadataFn != nil {
		return f.setMetadataFn(ctx, key, metadata)
	}
	return nil
}

func TestMetadataStorageGet(t *testing.T) {
	c := qt.New(t)

	key := types.HexBytes("key")
	want := testMetadata()

	c.Run("first provider wins", func(c *qt.C) {
		first := &fakeMetadataProvider{
			metadataFn: func(context.Context, types.HexBytes) (*types.Metadata, error) {
				return want, nil
			},
		}
		second := &fakeMetadataProvider{
			metadataFn: func(context.Context, types.HexBytes) (*types.Metadata, error) {
				return nil, fmt.Errorf("should not be called")
			},
		}

		storage := New(func(any) (types.HexBytes, error) { return key, nil }, first, second)
		got, err := storage.Get(context.Background(), key)
		c.Assert(err, qt.IsNil)
		c.Assert(got, qt.DeepEquals, want)
		c.Assert(first.metadataCalls, qt.Equals, 1)
		c.Assert(second.metadataCalls, qt.Equals, 0)
	})

	c.Run("falls through until success", func(c *qt.C) {
		first := &fakeMetadataProvider{
			metadataFn: func(context.Context, types.HexBytes) (*types.Metadata, error) {
				return nil, ErrNotFound
			},
		}
		second := &fakeMetadataProvider{
			metadataFn: func(context.Context, types.HexBytes) (*types.Metadata, error) {
				return want, nil
			},
		}

		storage := New(func(any) (types.HexBytes, error) { return key, nil }, first, second)
		got, err := storage.Get(context.Background(), key)
		c.Assert(err, qt.IsNil)
		c.Assert(got, qt.DeepEquals, want)
		c.Assert(first.metadataCalls, qt.Equals, 1)
		c.Assert(second.metadataCalls, qt.Equals, 1)
	})

	c.Run("returns not found when all providers fail", func(c *qt.C) {
		first := &fakeMetadataProvider{
			metadataFn: func(context.Context, types.HexBytes) (*types.Metadata, error) {
				return nil, ErrNotFound
			},
		}
		second := &fakeMetadataProvider{
			metadataFn: func(context.Context, types.HexBytes) (*types.Metadata, error) {
				return nil, ErrNotFound
			},
		}

		storage := New(func(any) (types.HexBytes, error) { return key, nil }, first, second)
		got, err := storage.Get(context.Background(), key)
		c.Assert(got, qt.IsNil)
		c.Assert(err, qt.ErrorIs, ErrNotFound)
	})

	c.Run("aggregates non not-found provider errors", func(c *qt.C) {
		first := &fakeMetadataProvider{
			metadataFn: func(context.Context, types.HexBytes) (*types.Metadata, error) {
				return nil, ErrNotFound
			},
		}
		boom := fmt.Errorf("boom")
		second := &fakeMetadataProvider{
			metadataFn: func(context.Context, types.HexBytes) (*types.Metadata, error) {
				return nil, boom
			},
		}

		storage := New(func(any) (types.HexBytes, error) { return key, nil }, first, second)
		got, err := storage.Get(context.Background(), key)
		c.Assert(got, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "failed to get metadata from providers")
		c.Assert(err, qt.ErrorIs, boom)
	})

	c.Run("requires key provider", func(c *qt.C) {
		storage := New(nil, &fakeMetadataProvider{})

		got, err := storage.Get(context.Background(), key)
		c.Assert(got, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "metadata key provider is not configured")
	})
}

func TestMetadataStorageSet(t *testing.T) {
	c := qt.New(t)

	key := types.HexBytes("computed-key")
	metadata := testMetadata()

	c.Run("returns key provider error", func(c *qt.C) {
		expectedErr := fmt.Errorf("key failed")
		storage := New(
			func(any) (types.HexBytes, error) { return nil, expectedErr },
			&fakeMetadataProvider{
				metadataFn: func(context.Context, types.HexBytes) (*types.Metadata, error) {
					return nil, ErrNotFound
				},
			},
		)

		gotKey, err := storage.Set(context.Background(), metadata)
		c.Assert(gotKey, qt.IsNil)
		c.Assert(err, qt.ErrorIs, expectedErr)
	})

	c.Run("skips providers that already have metadata", func(c *qt.C) {
		existing := &fakeMetadataProvider{
			metadataFn: func(context.Context, types.HexBytes) (*types.Metadata, error) {
				return metadata, nil
			},
		}
		missing := &fakeMetadataProvider{
			metadataFn: func(context.Context, types.HexBytes) (*types.Metadata, error) {
				return nil, ErrNotFound
			},
		}

		storage := New(func(any) (types.HexBytes, error) { return key, nil }, existing, missing)
		gotKey, err := storage.Set(context.Background(), metadata)
		c.Assert(err, qt.IsNil)
		c.Assert(gotKey, qt.DeepEquals, key)
		c.Assert(existing.setCalls, qt.Equals, 0)
		c.Assert(missing.setCalls, qt.Equals, 1)
	})

	c.Run("aggregates provider errors and returns key", func(c *qt.C) {
		errA := fmt.Errorf("provider a failed")
		errB := fmt.Errorf("provider b failed")
		first := &fakeMetadataProvider{
			metadataFn: func(context.Context, types.HexBytes) (*types.Metadata, error) {
				return nil, ErrNotFound
			},
			setMetadataFn: func(context.Context, types.HexBytes, *types.Metadata) error {
				return errA
			},
		}
		second := &fakeMetadataProvider{
			metadataFn: func(context.Context, types.HexBytes) (*types.Metadata, error) {
				return nil, ErrNotFound
			},
			setMetadataFn: func(context.Context, types.HexBytes, *types.Metadata) error {
				return errB
			},
		}

		storage := New(func(any) (types.HexBytes, error) { return key, nil }, first, second)
		gotKey, err := storage.Set(context.Background(), metadata)
		c.Assert(gotKey, qt.DeepEquals, key)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "some providers failed")
		c.Assert(err, qt.ErrorIs, errA)
		c.Assert(err, qt.ErrorIs, errB)
	})

	c.Run("returns error when there are no providers", func(c *qt.C) {
		storage := New(func(any) (types.HexBytes, error) { return key, nil })

		gotKey, err := storage.Set(context.Background(), metadata)
		c.Assert(gotKey, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "no metadata providers are configured")
	})

	c.Run("requires key provider", func(c *qt.C) {
		storage := New(nil, &fakeMetadataProvider{})

		gotKey, err := storage.Set(context.Background(), metadata)
		c.Assert(gotKey, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "metadata key provider is not configured")
	})
}
