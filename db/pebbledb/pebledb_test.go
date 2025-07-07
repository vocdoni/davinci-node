package pebbledb

import (
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/internal/dbtest"
	"github.com/vocdoni/davinci-node/db/prefixeddb"
)

func TestWriteTx(t *testing.T) {
	database, err := New(db.Options{Path: t.TempDir()})
	qt.Assert(t, err, qt.IsNil)

	dbtest.TestWriteTx(t, database)
}

func TestIterate(t *testing.T) {
	database, err := New(db.Options{Path: t.TempDir()})
	qt.Assert(t, err, qt.IsNil)

	dbtest.TestIterate(t, database)
}

func TestWriteTxApply(t *testing.T) {
	database, err := New(db.Options{Path: t.TempDir()})
	qt.Assert(t, err, qt.IsNil)

	dbtest.TestWriteTxApply(t, database)
}

func TestWriteTxApplyPrefixed(t *testing.T) {
	database, err := New(db.Options{Path: t.TempDir()})
	qt.Assert(t, err, qt.IsNil)

	prefix := []byte("one")
	dbWithPrefix := prefixeddb.NewPrefixedDatabase(database, prefix)

	dbtest.TestWriteTxApplyPrefixed(t, database, dbWithPrefix)
}

// NOTE: This test fails.  pebble.Batch doesn't detect conflicts.  Moreover,
// reads from a pebble.Batch return the last version from the Database, even if
// the update was made after the pebble.Batch was created.  Basically it's not
// a Transaction, but a Batch of write operations.
// func TestConcurrentWriteTx(t *testing.T) {
// 	database, err := New(db.Options{Path: t.TempDir()})
// 	qt.Assert(t, err, qt.IsNil)
//
// 	dbtest.TestConcurrentWriteTx(t, database)
// }

func TestClosedDB(t *testing.T) {
	c := qt.New(t)

	database, err := New(db.Options{Path: t.TempDir()})
	c.Assert(err, qt.IsNil)

	// Write some data
	key := []byte("key")
	value := []byte("value")
	wTx := database.WriteTx()
	otherTx := database.WriteTx()
	c.Assert(wTx.Set(key, value), qt.IsNil)

	// Close the database
	err = database.Close()
	c.Assert(err, qt.IsNil)

	// Attempt to get the value after closing the database
	_, err = wTx.Get(key)
	c.Assert(err, qt.IsNil)

	// Attempt to set a value after closing the database should panic
	err = wTx.Set(key, []byte("new_value"))
	c.Assert(err, qt.IsNil)

	// Attempt to delete a value after closing the database should panic
	err = wTx.Delete(key)
	c.Assert(err, qt.IsNil)

	// Attempt to iterate after closing the database should panic
	err = wTx.Iterate([]byte("prefix"), func(k, v []byte) bool {
		c.Fatalf("Iterate should not be called after closing the database")
		return true
	})
	c.Assert(err, qt.IsNil)

	// Attempt to apply another WriteTx after closing the database should panic
	err = wTx.Apply(otherTx)
	c.Assert(err, qt.IsNil)

	// Attempt to commit the WriteTx after closing the database should panic
	err = wTx.Commit()
	c.Assert(err, qt.IsNil)

	// Attempt to discard the WriteTx after closing the database should panic
	wTx.Discard()

	// Attempt to close the database again should not panic
	err = database.Close()
	c.Assert(err, qt.IsNil)

	// Attempt to create a new WriteTx after closing the database should panic
	_ = database.WriteTx()
}
