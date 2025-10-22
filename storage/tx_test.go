package storage

import (
	"path/filepath"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/util"
)

func TestPendingTxs(t *testing.T) {
	c := qt.New(t)

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	testProcessID := util.RandomBytes(32)

	c.Run("no pending txs", func(c *qt.C) {
		hasPending := st.HasPendingTx(StateTransitionTx, testProcessID)
		c.Assert(hasPending, qt.Equals, false)
	})

	c.Run("add pending tx", func(c *qt.C) {
		err := st.SetPendingTx(StateTransitionTx, testProcessID)
		c.Assert(err, qt.IsNil)

		hasPending := st.HasPendingTx(StateTransitionTx, testProcessID)
		c.Assert(hasPending, qt.Equals, true)

		// adding again should do nothing
		err = st.SetPendingTx(StateTransitionTx, testProcessID)
		c.Assert(err, qt.IsNil)

		hasPending = st.HasPendingTx(StateTransitionTx, testProcessID)
		c.Assert(hasPending, qt.Equals, true)
	})

	c.Run("release pending tx", func(c *qt.C) {
		err := st.SetPendingTx(StateTransitionTx, testProcessID)
		c.Assert(err, qt.IsNil)

		hasPending := st.HasPendingTx(StateTransitionTx, testProcessID)
		c.Assert(hasPending, qt.Equals, true)

		err = st.PrunePendingTx(StateTransitionTx, testProcessID)
		c.Assert(err, qt.IsNil)

		hasPending = st.HasPendingTx(StateTransitionTx, testProcessID)
		c.Assert(hasPending, qt.Equals, false)
	})
}
