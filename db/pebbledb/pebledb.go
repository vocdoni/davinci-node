package pebbledb

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/cockroachdb/pebble"
	"github.com/vocdoni/davinci-node/db"
)

// WriteTx implements the interface db.WriteTx
type WriteTx struct {
	batch *pebble.Batch
}

// check that WriteTx implements the db.WriteTx interface
var _ db.WriteTx = (*WriteTx)(nil)

func get(reader pebble.Reader, k []byte) ([]byte, error) {
	defer handleClosedDBPanic()
	v, closer, err := reader.Get(k)
	if errors.Is(err, pebble.ErrNotFound) {
		return nil, db.ErrKeyNotFound
	}
	if err != nil {
		return nil, err
	}

	// Note that the returned value slice is only valid until Close is called.
	// Make a copy so we can return it.
	// TODO(mvdan): write a dbtest test to ensure this property on all DBs.
	v2 := bytes.Clone(v)

	if err := closer.Close(); err != nil {
		return nil, err
	}
	return v2, nil
}

func iterate(reader pebble.Reader, prefix []byte, callback func(k, v []byte) bool) (err error) {
	defer handleClosedDBPanic()
	iterOptions := &pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: keyUpperBound(prefix),
	}
	iter, err := reader.NewIter(iterOptions)
	if err != nil {
		return err
	}
	defer func() {
		errC := iter.Close()
		if err != nil {
			return
		}
		err = errC
	}()

	for iter.First(); iter.Valid(); iter.Next() {
		localKey := iter.Key()[len(prefix):]
		if cont := callback(localKey, iter.Value()); !cont {
			break
		}
	}
	return iter.Error()
}

// Get implements the db.WriteTx.Get interface method
func (tx *WriteTx) Get(k []byte) ([]byte, error) {
	return get(tx.batch, k)
}

func (tx *WriteTx) Iterate(prefix []byte, callback func(k, v []byte) bool) (err error) {
	return iterate(tx.batch, prefix, callback)
}

// Set implements the db.WriteTx.Set interface method
func (tx *WriteTx) Set(k, v []byte) error {
	defer handleClosedDBPanic()
	return tx.batch.Set(k, v, nil)
}

// Delete implements the db.WriteTx.Delete interface method
func (tx *WriteTx) Delete(k []byte) error {
	defer handleClosedDBPanic()
	return tx.batch.Delete(k, nil)
}

// Apply implements the db.WriteTx.Apply interface method
func (tx *WriteTx) Apply(other db.WriteTx) (err error) {
	defer handleClosedDBPanic()
	otherPebble := db.UnwrapWriteTx(other).(*WriteTx)
	return tx.batch.Apply(otherPebble.batch, nil)
}

// Commit implements the db.WriteTx.Commit interface method
func (tx *WriteTx) Commit() error {
	defer handleClosedDBPanic()
	if tx.batch == nil {
		return fmt.Errorf("cannot commit pebble tx: already committed or discarded")
	}
	err := tx.batch.Commit(nil)
	tx.batch = nil
	return err
}

// Discard implements the db.WriteTx.Discard interface method
func (tx *WriteTx) Discard() {
	if tx.batch == nil {
		// Silently allow discarding twice or after a commit,
		// since it can help for the sake of defers.
		// Note that upstream pebbledb doesn't really allow calling Close twice;
		// the object gets put in a shared pool, so a later call to Close races
		// with another goroutine having already reused the same object from the pool.
		return
	}
	// Close returns an error, but here in the Discard context is omitted
	_ = tx.batch.Close()
	tx.batch = nil
}

// PebbleDB implements db.Database interface
type PebbleDB struct {
	db *pebble.DB
}

// check that PebbleDB implements the db.Database interface
var _ db.Database = (*PebbleDB)(nil)

// New returns a PebbleDB using the given Options, which implements the
// db.Database interface
func New(opts db.Options) (*PebbleDB, error) {
	if err := os.MkdirAll(opts.Path, os.ModePerm); err != nil {
		return nil, err
	}
	o := &pebble.Options{
		Levels: []pebble.LevelOptions{
			{
				Compression: pebble.SnappyCompression,
			},
		},
	}
	db, err := pebble.Open(opts.Path, o)
	if err != nil {
		return nil, err
	}

	return &PebbleDB{
		db: db,
	}, nil
}

// Get implements the db.WriteTx.Get interface method
func (db *PebbleDB) Get(k []byte) ([]byte, error) {
	return get(db.db, k)
}

// WriteTx returns a db.WriteTx
func (db *PebbleDB) WriteTx() db.WriteTx {
	return &WriteTx{
		batch: db.db.NewIndexedBatch(),
	}
}

// Close closes the PebbleDB
func (db *PebbleDB) Close() error {
	defer handleClosedDBPanic()
	return db.db.Close()
}

func keyUpperBound(b []byte) []byte {
	// https://github.com/cockroachdb/pebble/blob/b2eb88a7182687c81d911c425309ef0e1f545452/iterator_example_test.go#L44
	end := bytes.Clone(b)
	for i := len(end) - 1; i >= 0; i-- {
		end[i] = end[i] + 1
		if end[i] != 0 {
			return end[:i+1]
		}
	}
	return nil // no upper-bound
}

// Iterate implements the db.Database.Iterate interface method
func (db *PebbleDB) Iterate(prefix []byte, callback func(k, v []byte) bool) (err error) {
	return iterate(db.db, prefix, callback)
}

// Compact implements the db.Database.Compact interface method
func (db *PebbleDB) Compact() error {
	defer handleClosedDBPanic()
	// from https://github.com/cockroachdb/pebble/issues/1474#issuecomment-1022313365
	iter, err := db.db.NewIter(nil)
	if err != nil {
		return err
	}
	var first, last []byte
	if iter.First() {
		first = append(first, iter.Key()...)
	}
	if iter.Last() {
		last = append(last, iter.Key()...)
	}
	if err := iter.Close(); err != nil {
		return err
	}
	return db.db.Compact(first, last, true)
}

// handleClosedDBPanic wraps the commit operation to handle panic scenarios
// about already closed database. It should be deferred in any storage
// operation that might read, write or commit to the database that can be
// closed during the operation.
func handleClosedDBPanic() {
	if r := recover(); r != nil {
		// Collect stack trace
		stack := []string{}
		for i := range 32 {
			pc, file, line, ok := runtime.Caller(i)
			if !ok {
				break
			}
			fn := runtime.FuncForPC(pc)
			funcName := ""
			if fn != nil {
				funcName = fn.Name()
			}
			stack = append(stack, fmt.Sprintf("%s\n\t%s:%d", funcName, file, line))
		}

		// Check if the panic is due to a closed database
		if strings.Contains(fmt.Sprintf("%v", r), "closed") {
			return
		}
		// If it's not a closed database panic, re-panic with the stack trace
		panic(fmt.Sprintf("panic during storage operation: %v: %s", r, strings.Join(stack, "\n")))
	}
}
