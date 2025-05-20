package storage

import (
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/metadb"
)

func TestEncryptionKeys(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	db, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	st := New(db)
	defer st.Close()

	// Create test process IDs
	processID1 := &types.ProcessID{
		Address: common.Address{1},
		Nonce:   42,
		ChainID: 1,
	}

	processID2 := &types.ProcessID{
		Address: common.Address{2},
		Nonce:   43,
		ChainID: 1,
	}

	// Test 1: Initially no encryption keys exist
	pids, err := st.ListProcessWithEncryptionKeys()
	c.Assert(err, qt.IsNil)
	c.Assert(len(pids), qt.Equals, 0)

	// Generate a key pair for processID1
	publicKey1, privateKey1, err := elgamal.GenerateKey(bjj.New())
	c.Assert(err, qt.IsNil)

	// Test 2: Store encryption keys for processID1
	err = st.SetEncryptionKeys(processID1, publicKey1, privateKey1)
	c.Assert(err, qt.IsNil)

	// Verify we can retrieve the keys
	pubKey, privKey, err := st.EncryptionKeys(processID1)
	c.Assert(err, qt.IsNil)
	x1, y1 := publicKey1.Point()
	x2, y2 := pubKey.Point()
	c.Assert(x1.Cmp(x2), qt.Equals, 0)
	c.Assert(y1.Cmp(y2), qt.Equals, 0)
	c.Assert(privateKey1.Cmp(privKey), qt.Equals, 0)

	// Test 3: List encryption keys with one process
	pids, err = st.ListProcessWithEncryptionKeys()
	c.Assert(err, qt.IsNil)
	c.Assert(len(pids), qt.Equals, 1)
	c.Assert(string(pids[0]), qt.DeepEquals, string(processID1.Marshal()))

	// Generate a key pair for processID2
	publicKey2, privateKey2, err := elgamal.GenerateKey(bjj.New())
	c.Assert(err, qt.IsNil)

	// Test 4: Store encryption keys for processID2
	err = st.SetEncryptionKeys(processID2, publicKey2, privateKey2)
	c.Assert(err, qt.IsNil)

	// Test 5: List encryption keys with two processes
	pids, err = st.ListProcessWithEncryptionKeys()
	c.Assert(err, qt.IsNil)
	c.Assert(len(pids), qt.Equals, 2)

	// Verify both processes are in the list
	// Sort the pids to ensure consistent comparison
	foundProcessID1 := false
	foundProcessID2 := false

	for _, pid := range pids {
		if string(pid) == string(processID1.Marshal()) {
			foundProcessID1 = true
		}
		if string(pid) == string(processID2.Marshal()) {
			foundProcessID2 = true
		}
	}

	c.Assert(foundProcessID1, qt.IsTrue)
	c.Assert(foundProcessID2, qt.IsTrue)
}
