package metadata

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/types"
)

type fakeDB struct {
	getFn     func([]byte) ([]byte, error)
	iterateFn func([]byte, func([]byte, []byte) bool) error
	writeTx   db.WriteTx
}

func (f *fakeDB) Close() error {
	return nil
}

func (f *fakeDB) Get(key []byte) ([]byte, error) {
	if f.getFn != nil {
		return f.getFn(key)
	}
	return nil, db.ErrKeyNotFound
}

func (f *fakeDB) Iterate(prefix []byte, callback func([]byte, []byte) bool) error {
	if f.iterateFn != nil {
		return f.iterateFn(prefix, callback)
	}
	return nil
}

func (f *fakeDB) WriteTx() db.WriteTx {
	if f.writeTx != nil {
		return f.writeTx
	}
	return &fakeWriteTx{}
}

func (f *fakeDB) Compact() error {
	return nil
}

type fakeWriteTx struct {
	getFn     func([]byte) ([]byte, error)
	iterateFn func([]byte, func([]byte, []byte) bool) error
	setFn     func([]byte, []byte) error
	deleteFn  func([]byte) error
	applyFn   func(db.WriteTx) error
	commitFn  func() error
	discardFn func()
	discarded bool
}

func (f *fakeWriteTx) Get(key []byte) ([]byte, error) {
	if f.getFn != nil {
		return f.getFn(key)
	}
	return nil, db.ErrKeyNotFound
}

func (f *fakeWriteTx) Iterate(prefix []byte, callback func([]byte, []byte) bool) error {
	if f.iterateFn != nil {
		return f.iterateFn(prefix, callback)
	}
	return nil
}

func (f *fakeWriteTx) Set(key, value []byte) error {
	if f.setFn != nil {
		return f.setFn(key, value)
	}
	return nil
}

func (f *fakeWriteTx) Delete(key []byte) error {
	if f.deleteFn != nil {
		return f.deleteFn(key)
	}
	return nil
}

func (f *fakeWriteTx) Apply(other db.WriteTx) error {
	if f.applyFn != nil {
		return f.applyFn(other)
	}
	return nil
}

func (f *fakeWriteTx) Commit() error {
	if f.commitFn != nil {
		return f.commitFn()
	}
	return nil
}

func (f *fakeWriteTx) Discard() {
	f.discarded = true
	if f.discardFn != nil {
		f.discardFn()
	}
}

func TestNewLocalMetadata(t *testing.T) {
	c := qt.New(t)

	lm := NewLocalMetadata(metadb.NewTest(t))
	c.Assert(lm.cache, qt.IsNotNil)
}

func TestLocalMetadataSetMetadataNil(t *testing.T) {
	c := qt.New(t)

	lm := NewLocalMetadata(metadb.NewTest(t))
	err := lm.SetMetadata(context.Background(), types.HexBytes("key"), nil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "nil metadata")
}

func TestLocalMetadataMetadataNilKey(t *testing.T) {
	c := qt.New(t)

	lm := NewLocalMetadata(metadb.NewTest(t))
	metadata, err := lm.Metadata(context.Background(), nil)
	c.Assert(metadata, qt.IsNil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "no key provider")
}

func TestLocalMetadataRoundTrip(t *testing.T) {
	c := qt.New(t)

	lm := NewLocalMetadata(metadb.NewTest(t))
	metadata := testMetadata()
	key, _, err := CID(metadata)
	c.Assert(err, qt.IsNil)

	err = lm.SetMetadata(context.Background(), key, metadata)
	c.Assert(err, qt.IsNil)

	got, err := lm.Metadata(context.Background(), key)
	c.Assert(err, qt.IsNil)
	c.Assert(got, qt.DeepEquals, metadata)

	cacheKey := string(metadataPrefix) + key.Hex()
	cached, ok := lm.cache.Get(cacheKey)
	c.Assert(ok, qt.IsTrue)
	c.Assert(cached, qt.Equals, got)
}

func TestLocalMetadataMetadataUsesCache(t *testing.T) {
	c := qt.New(t)

	lm := NewLocalMetadata(&fakeDB{
		getFn: func([]byte) ([]byte, error) {
			return nil, fmt.Errorf("database should not be touched")
		},
	})
	key := types.HexBytes("cached-key")
	want := testMetadata()
	lm.cache.Add(string(metadataPrefix)+key.Hex(), want)

	got, err := lm.Metadata(context.Background(), key)
	c.Assert(err, qt.IsNil)
	c.Assert(got, qt.Equals, want)
}

func TestLocalMetadataWrongTypeCacheFallsBackToStorage(t *testing.T) {
	c := qt.New(t)

	lm := NewLocalMetadata(metadb.NewTest(t))
	metadata := testMetadata()
	key, _, err := CID(metadata)
	c.Assert(err, qt.IsNil)
	c.Assert(lm.SetMetadata(context.Background(), key, metadata), qt.IsNil)

	cacheKey := string(metadataPrefix) + key.Hex()
	lm.cache.Add(cacheKey, "wrong-type")

	got, err := lm.Metadata(context.Background(), key)
	c.Assert(err, qt.IsNil)
	c.Assert(got, qt.DeepEquals, metadata)

	cached, ok := lm.cache.Get(cacheKey)
	c.Assert(ok, qt.IsTrue)
	typed, ok := cached.(*types.Metadata)
	c.Assert(ok, qt.IsTrue)
	c.Assert(typed, qt.DeepEquals, metadata)
}

func TestLocalMetadataMetadataMissingKey(t *testing.T) {
	c := qt.New(t)

	lm := NewLocalMetadata(metadb.NewTest(t))
	metadata, err := lm.Metadata(context.Background(), types.HexBytes("missing"))
	c.Assert(metadata, qt.IsNil)
	c.Assert(err, qt.ErrorIs, ErrNotFound)
}

func TestLocalMetadataGetValueFirstIteratedValue(t *testing.T) {
	c := qt.New(t)

	lm := NewLocalMetadata(metadb.NewTest(t))
	first := testMetadata()
	first.Version = "first"
	second := testMetadata()
	second.Version = "second"

	c.Assert(lm.setValue(metadataPrefix, types.HexBytes("b"), second), qt.IsNil)
	c.Assert(lm.setValue(metadataPrefix, types.HexBytes("a"), first), qt.IsNil)

	var got types.Metadata
	err := lm.getValue(metadataPrefix, nil, &got)
	c.Assert(err, qt.IsNil)
	c.Assert(&got, qt.DeepEquals, first)
}

func TestLocalMetadataGetValueNoEntries(t *testing.T) {
	c := qt.New(t)

	lm := NewLocalMetadata(metadb.NewTest(t))
	var got types.Metadata
	err := lm.getValue(metadataPrefix, nil, &got)
	c.Assert(err, qt.ErrorIs, ErrNotFound)
}

func TestLocalMetadataGetValueDecodeError(t *testing.T) {
	c := qt.New(t)

	lm := NewLocalMetadata(&fakeDB{
		getFn: func(key []byte) ([]byte, error) {
			c.Assert(bytes.HasPrefix(key, metadataPrefix), qt.IsTrue)
			return []byte("not-json"), nil
		},
	})

	var got types.Metadata
	err := lm.getValue(metadataPrefix, types.HexBytes("key"), &got)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "could not decode artifact")
}

func TestLocalMetadataGetValueIterateError(t *testing.T) {
	c := qt.New(t)

	expectedErr := fmt.Errorf("iterate failed")
	lm := NewLocalMetadata(&fakeDB{
		iterateFn: func(prefix []byte, callback func([]byte, []byte) bool) error {
			c.Assert(bytes.Equal(prefix, metadataPrefix), qt.IsTrue)
			return expectedErr
		},
	})

	var got types.Metadata
	err := lm.getValue(metadataPrefix, nil, &got)
	c.Assert(err, qt.ErrorIs, expectedErr)
}

func TestLocalMetadatasetValueMarshalError(t *testing.T) {
	c := qt.New(t)

	lm := NewLocalMetadata(metadb.NewTest(t))
	err := lm.setValue(metadataPrefix, types.HexBytes("key"), map[string]any{"bad": func() {}})
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "error encoding value")
}

func TestLocalMetadatasetValueSetError(t *testing.T) {
	c := qt.New(t)

	expectedErr := fmt.Errorf("set failed")
	tx := &fakeWriteTx{
		setFn: func(key, value []byte) error {
			c.Assert(bytes.HasPrefix(key, metadataPrefix), qt.IsTrue)
			c.Assert(len(value) > 0, qt.IsTrue)
			return expectedErr
		},
	}
	lm := NewLocalMetadata(&fakeDB{writeTx: tx})

	err := lm.setValue(metadataPrefix, types.HexBytes("key"), testMetadata())
	c.Assert(err, qt.ErrorIs, expectedErr)
	c.Assert(tx.discarded, qt.IsTrue)
}

func TestLocalMetadatasetValueCommitError(t *testing.T) {
	c := qt.New(t)

	expectedErr := fmt.Errorf("commit failed")
	tx := &fakeWriteTx{
		commitFn: func() error {
			return expectedErr
		},
	}
	lm := NewLocalMetadata(&fakeDB{writeTx: tx})

	err := lm.setValue(metadataPrefix, types.HexBytes("key"), testMetadata())
	c.Assert(err, qt.ErrorIs, expectedErr)
	c.Assert(tx.discarded, qt.IsTrue)
}
