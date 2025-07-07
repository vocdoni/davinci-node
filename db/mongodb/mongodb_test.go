package mongodb

import (
	"fmt"
	"os"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/internal/dbtest"
	"github.com/vocdoni/davinci-node/db/prefixeddb"
	"github.com/vocdoni/davinci-node/util"
)

func TestWriteTx(t *testing.T) {
	if os.Getenv("MONGODB_URL") == "" {
		t.Skip("the mongodb driver isn't complete")
	}
	database, err := New(db.Options{Path: util.RandomHex(16)})
	qt.Assert(t, err, qt.IsNil)

	dbtest.TestWriteTx(t, database)
}

func TestIterate(t *testing.T) {
	if os.Getenv("MONGODB_URL") == "" {
		t.Skip("the mongodb driver isn't complete")
	}
	database, err := New(db.Options{Path: util.RandomHex(16)})
	qt.Assert(t, err, qt.IsNil)

	dbtest.TestIterate(t, database)
}

func TestWriteTxApply(t *testing.T) {
	if os.Getenv("MONGODB_URL") == "" {
		t.Skip("the mongodb driver isn't complete")
	}
	database, err := New(db.Options{Path: util.RandomHex(16)})
	qt.Assert(t, err, qt.IsNil)

	dbtest.TestWriteTxApply(t, database)
}

func TestWriteTxApplyPrefixed(t *testing.T) {
	if os.Getenv("MONGODB_URL") == "" {
		t.Skip("the mongodb driver isn't complete")
	}
	database, err := New(db.Options{Path: util.RandomHex(16)})
	qt.Assert(t, err, qt.IsNil)

	prefix := []byte("one")
	dbWithPrefix := prefixeddb.NewPrefixedDatabase(database, prefix)

	dbtest.TestWriteTxApplyPrefixed(t, database, dbWithPrefix)
}

func BenchmarkWriteTx(b *testing.B) {
	database, err := New(db.Options{Path: b.TempDir()})
	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		if err := database.Close(); err != nil {
			b.Error(err)
		}
	}()

	for b.Loop() {
		tx := database.WriteTx()
		if err := tx.Set([]byte("key"), []byte("value")); err != nil {
			b.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIterate(b *testing.B) {
	database, err := New(db.Options{Path: util.RandomHex(16)})
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			b.Error(err)
		}
	}()

	tx := database.WriteTx()
	for i := range 100000 {
		if err := tx.Set(fmt.Appendf(nil, "key%d", i), []byte("value")); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}

	for b.Loop() {
		err = database.Iterate([]byte("key"), func(k, v []byte) bool {
			return true
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteTxApply(b *testing.B) {
	database, err := New(db.Options{Path: util.RandomHex(16)})
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			b.Error(err)
		}
	}()

	tx1 := database.WriteTx()
	if err := tx1.Set([]byte("key1"), []byte("value1")); err != nil {
		b.Fatal(err)
	}

	tx2 := database.WriteTx()
	if err := tx2.Set([]byte("key2"), []byte("value2")); err != nil {
		b.Fatal(err)
	}

	for b.Loop() {
		if err := tx1.Apply(tx2); err != nil {
			b.Fatal(err)
		}
		if err := tx1.Commit(); err != nil {
			b.Fatal(err)
		}
	}
}
