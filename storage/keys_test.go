package storage

// func TestEncryptionKeys(t *testing.T) {
// 	c := qt.New(t)
// 	tempDir := t.TempDir()
// 	dbPath := filepath.Join(tempDir, "db")

// 	db, err := metadb.New(db.TypePebble, dbPath)
// 	c.Assert(err, qt.IsNil)

// 	st := New(db)
// 	defer st.Close()

// 	// Create test process IDs
// 	processID1 := testutil.DeterministicProcessID(42)

// 	processID2 := testutil.DeterministicProcessID(43)

// 	// Test 1: Initially no encryption keys exist
// 	pids, err := st.ListProcessWithEncryptionKeys()
// 	c.Assert(err, qt.IsNil)
// 	c.Assert(len(pids), qt.Equals, 0)

// 	// Generate a key pair for processID1
// 	publicKey1, privateKey1, err := elgamal.GenerateKey(bjj.New())
// 	c.Assert(err, qt.IsNil)

// 	// Test 2: Store encryption keys for processID1
// 	err = st.SetEncryptionKeys(processID1, publicKey1, privateKey1)
// 	c.Assert(err, qt.IsNil)

// 	// Verify we can retrieve the keys
// 	pubKey, privKey, err := st.EncryptionKeys(processID1)
// 	c.Assert(err, qt.IsNil)
// 	x1, y1 := publicKey1.Point()
// 	x2, y2 := pubKey.Point()
// 	c.Assert(x1.Cmp(x2), qt.Equals, 0)
// 	c.Assert(y1.Cmp(y2), qt.Equals, 0)
// 	c.Assert(privateKey1.Cmp(privKey), qt.Equals, 0)

// 	// Test 3: List encryption keys with one process
// 	pids, err = st.ListProcessWithEncryptionKeys()
// 	c.Assert(err, qt.IsNil)
// 	c.Assert(len(pids), qt.Equals, 1)
// 	c.Assert(pids[0].Bytes(), qt.DeepEquals, processID1.Bytes())

// 	// Generate a key pair for processID2
// 	publicKey2, privateKey2, err := elgamal.GenerateKey(bjj.New())
// 	c.Assert(err, qt.IsNil)

// 	// Test 4: Store encryption keys for processID2
// 	err = st.SetEncryptionKeys(processID2, publicKey2, privateKey2)
// 	c.Assert(err, qt.IsNil)

// 	// Test 5: List encryption keys with two processes
// 	pids, err = st.ListProcessWithEncryptionKeys()
// 	c.Assert(err, qt.IsNil)
// 	c.Assert(len(pids), qt.Equals, 2)

// 	// Verify both processes are in the list
// 	c.Assert(slices.Contains(pids, processID1), qt.IsTrue)
// 	c.Assert(slices.Contains(pids, processID2), qt.IsTrue)
// }
