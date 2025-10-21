package census

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/google/uuid"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
)

// newDatabase returns a new in-memory test database.
func newDatabase(t *testing.T) db.Database {
	return metadb.NewTest(t)
}

// makeAddress creates a 20-byte address from a string for testing.
// It pads or truncates the input to exactly 20 bytes.
func makeAddress(s string) []byte {
	addr := make([]byte, 20)
	copy(addr, []byte(s))
	return addr
}

func TestNewCensusDB(t *testing.T) {
	t.Parallel()
	db := newDatabase(t)
	censusDB := NewCensusDB(db)
	qt.Assert(t, censusDB, qt.IsNotNil)
	qt.Assert(t, censusDB.db, qt.IsNotNil)
}

func TestCensusDBNew(t *testing.T) {
	t.Parallel()
	censusDB := NewCensusDB(newDatabase(t))
	censusID := uuid.New()

	censusRef, err := censusDB.New(censusID)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, censusRef, qt.IsNotNil)
	qt.Assert(t, censusRef.Tree(), qt.IsNotNil)
}

func TestCensusDBExists(t *testing.T) {
	t.Parallel()
	censusDB := NewCensusDB(newDatabase(t))
	censusID := uuid.New()

	// Before creation.
	existsBefore := censusDB.Exists(censusID)
	qt.Assert(t, existsBefore, qt.IsFalse)

	// Create a new census.
	_, err := censusDB.New(censusID)
	qt.Assert(t, err, qt.IsNil)

	existsAfter := censusDB.Exists(censusID)
	qt.Assert(t, existsAfter, qt.IsTrue)
}

func TestCensusDBDel(t *testing.T) {
	t.Parallel()
	censusDB := NewCensusDB(newDatabase(t))
	censusID := uuid.New()

	// Create a census for deletion.
	_, err := censusDB.New(censusID)
	qt.Assert(t, err, qt.IsNil)

	// Delete the census.
	err = censusDB.Del(censusID)
	qt.Assert(t, err, qt.IsNil)

	// Wait a bit since the deletion of the underlying tree is asynchronous.
	time.Sleep(1 * time.Second)

	// Check that the census is no longer accessible.
	existsAfter := censusDB.Exists(censusID)
	qt.Assert(t, existsAfter, qt.IsFalse)
}

func TestSequentialLoadReturnsSamePointer(t *testing.T) {
	t.Parallel()
	censusDB := NewCensusDB(newDatabase(t))
	censusID := uuid.New()

	ref1, err := censusDB.New(censusID)
	qt.Assert(t, err, qt.IsNil)

	ref2, err := censusDB.Load(censusID)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, ref1, qt.Equals, ref2)
}

func TestLoadNonExistingCensus(t *testing.T) {
	t.Parallel()
	censusDB := NewCensusDB(newDatabase(t))
	censusID := uuid.New() // Not created.

	ref, err := censusDB.Load(censusID)
	qt.Assert(t, ref, qt.IsNil)
	qt.Assert(t, err, qt.Not(qt.IsNil))
	qt.Assert(t, err.Error(), qt.Contains, "census not found")
}

func TestPersistenceAcrossCensusDBInstances(t *testing.T) {
	t.Parallel()
	db := newDatabase(t)
	censusID := uuid.New()

	censusDB1 := NewCensusDB(db)
	ref1, err := censusDB1.New(censusID)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, ref1, qt.IsNotNil)

	// Close the first tree to release the Pebble database lock.
	err = ref1.tree.Close()
	qt.Assert(t, err, qt.IsNil)

	// Create a new CensusDB instance sharing the same underlying database.
	censusDB2 := NewCensusDB(db)
	ref2, err := censusDB2.Load(censusID)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, ref2, qt.IsNotNil)
	qt.Assert(t, ref2.Tree(), qt.IsNotNil)
}

func TestLoadAfterDelete(t *testing.T) {
	t.Parallel()
	censusDB := NewCensusDB(newDatabase(t))
	censusID := uuid.New()

	_, err := censusDB.New(censusID)
	qt.Assert(t, err, qt.IsNil)

	err = censusDB.Del(censusID)
	qt.Assert(t, err, qt.IsNil)

	// Allow async deletion to complete.
	time.Sleep(100 * time.Millisecond)

	ref, err := censusDB.Load(censusID)
	qt.Assert(t, ref, qt.IsNil)
	qt.Assert(t, err, qt.Not(qt.IsNil))
	qt.Assert(t, err.Error(), qt.Contains, "census not found")
}

func TestCensusDBConcurrentLoad(t *testing.T) {
	censusDB := NewCensusDB(newDatabase(t))
	censusID := uuid.New()

	// Create the census.
	ref, err := censusDB.New(censusID)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, ref, qt.IsNotNil)

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Channels to collect results.
	errs := make(chan error, numGoroutines)
	refs := make(chan *CensusRef, numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()
			r, err := censusDB.Load(censusID)
			if err != nil {
				errs <- err
			} else {
				refs <- r
			}
		}()
	}
	wg.Wait()
	close(errs)
	close(refs)

	for err := range errs {
		qt.Assert(t, err, qt.IsNil)
	}

	var firstRef *CensusRef
	for r := range refs {
		if firstRef == nil {
			firstRef = r
		} else {
			qt.Assert(t, r, qt.Equals, firstRef)
		}
	}
}

func TestCensusDBConcurrentNew(t *testing.T) {
	censusDB := NewCensusDB(newDatabase(t))
	censusID := uuid.New()
	const numGoroutines = 20

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	var successCount int32
	var failureCount int32

	for range numGoroutines {
		go func() {
			defer wg.Done()
			ref, err := censusDB.New(censusID)
			if err == nil && ref != nil {
				atomic.AddInt32(&successCount, 1)
			} else if err != nil {
				// Only ErrCensusAlreadyExists is expected after one success.
				if err == ErrCensusAlreadyExists {
					atomic.AddInt32(&failureCount, 1)
				} else {
					t.Errorf("unexpected error: %v", err)
				}
			}
		}()
	}
	wg.Wait()

	qt.Assert(t, successCount, qt.Equals, int32(1))
	qt.Assert(t, failureCount, qt.Equals, int32(numGoroutines-1))
}

func TestConcurrentExists(t *testing.T) {
	censusDB := NewCensusDB(newDatabase(t))
	censusID := uuid.New()
	const numGoroutines = 20

	var wg sync.WaitGroup

	// Concurrently check Exists before the census is created.
	wg.Add(numGoroutines)
	existsBefore := make(chan bool, numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			existsBefore <- censusDB.Exists(censusID)
		}()
	}
	wg.Wait()
	close(existsBefore)
	for exists := range existsBefore {
		qt.Assert(t, exists, qt.IsFalse)
	}

	// Create the census.
	_, err := censusDB.New(censusID)
	qt.Assert(t, err, qt.IsNil)

	// Concurrently check Exists after creation.
	wg.Add(numGoroutines)
	existsAfter := make(chan bool, numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			existsAfter <- censusDB.Exists(censusID)
		}()
	}
	wg.Wait()
	close(existsAfter)
	for exists := range existsAfter {
		qt.Assert(t, exists, qt.IsTrue)
	}
}

func TestMultipleCensuses(t *testing.T) {
	censusDB := NewCensusDB(newDatabase(t))
	const numCensuses = 20
	var wg sync.WaitGroup
	censusIDs := make([]uuid.UUID, numCensuses)

	// Concurrently create several censuses.
	wg.Add(numCensuses)
	for i := range numCensuses {
		censusIDs[i] = uuid.New()
		go func(id uuid.UUID) {
			defer wg.Done()
			ref, err := censusDB.New(id)
			qt.Assert(t, err, qt.IsNil)
			qt.Assert(t, ref, qt.IsNotNil)
		}(censusIDs[i])
	}
	wg.Wait()

	// Concurrently load each census.
	wg.Add(numCensuses)
	for i := range numCensuses {
		go func(id uuid.UUID) {
			defer wg.Done()
			ref, err := censusDB.Load(id)
			qt.Assert(t, err, qt.IsNil)
			qt.Assert(t, ref, qt.IsNotNil)
		}(censusIDs[i])
	}
	wg.Wait()
}

func TestProofByRootNonExistentRoot(t *testing.T) {
	t.Parallel()
	censusDB := NewCensusDB(newDatabase(t))
	// Use a fake root that is not in the index.
	fakeRoot := []byte("deadbeef")
	leafKey := []byte("somekey")
	proof, err := censusDB.ProofByRoot(fakeRoot, leafKey)
	qt.Assert(t, proof, qt.IsNil)
	qt.Assert(t, err, qt.Not(qt.IsNil))
	qt.Assert(t, err.Error(), qt.Contains, "no census found")
}

func TestProofByRootNonExistentLeaf(t *testing.T) {
	t.Parallel()
	db := newDatabase(t)
	censusDB := NewCensusDB(db)
	censusID := uuid.New()
	ref, err := censusDB.New(censusID)
	qt.Assert(t, err, qt.IsNil)
	// Insert a known key/value pair.
	leafKey := makeAddress("existingKey")
	value := []byte("someValue")
	err = ref.Insert(leafKey, value)
	qt.Assert(t, err, qt.IsNil)

	// Now query with a non-existent leaf key.
	nonExistentLeaf := makeAddress("nonExistentKey")
	root := ref.Root()
	proof, err := censusDB.ProofByRoot(root, nonExistentLeaf)
	qt.Assert(t, proof, qt.IsNil)
	qt.Assert(t, err, qt.Not(qt.IsNil))
}

func TestProofByRootValid(t *testing.T) {
	t.Parallel()
	db := newDatabase(t)
	censusDB := NewCensusDB(db)
	censusID := uuid.New()
	ref, err := censusDB.New(censusID)
	qt.Assert(t, err, qt.IsNil)
	// Insert a key/value pair.
	leafKey := makeAddress("myKey")
	value := []byte("myValue")
	err = ref.Insert(leafKey, value)
	qt.Assert(t, err, qt.IsNil)

	// Use the new root to get a proof.
	newRoot := ref.Root()
	proof, err := censusDB.ProofByRoot(newRoot, leafKey)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, proof, qt.Not(qt.IsNil))
	qt.Assert(t, []byte(proof.Address), qt.DeepEquals, leafKey)
	// Note: proof.Value is the packed address+weight, not the raw value
	qt.Assert(t, proof.Weight, qt.IsNotNil)
}

func TestUpdateRootConcurrent(t *testing.T) {
	t.Parallel()
	db := newDatabase(t)
	censusDB := NewCensusDB(db)
	censusID := uuid.New()
	ref, err := censusDB.New(censusID)
	qt.Assert(t, err, qt.IsNil)

	// Concurrently insert new key/value pairs and update the root index.
	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for i := range numGoroutines {
		go func(i int) {
			ref2, err := censusDB.Load(censusID)
			qt.Assert(t, err, qt.IsNil)
			defer wg.Done()
			for j := 0; j < 20; j++ {
				key := makeAddress(fmt.Sprintf("key%d%d", i, j))
				val := []byte(fmt.Sprintf("val%d%d", i, j))
				_ = ref2.Insert(key, val)
			}
		}(i)
	}
	wg.Wait()

	// After concurrent updates, try generating a proof for one key.
	testKey := makeAddress("key00")
	proof, err := censusDB.ProofByRoot(ref.Root(), testKey)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, ref.Root(), qt.IsNotNil)
	qt.Assert(t, proof, qt.Not(qt.IsNil))
}

func TestSameRootForMultipleCensuses(t *testing.T) {
	t.Parallel()
	db := newDatabase(t)
	censusDB := NewCensusDB(db)
	// Create two censuses with identical insertions.
	censusID1 := uuid.New()
	censusID2 := uuid.New()
	ref1, err := censusDB.New(censusID1)
	qt.Assert(t, err, qt.IsNil)
	ref2, err := censusDB.New(censusID2)
	qt.Assert(t, err, qt.IsNil)

	// Insert the same key/value pair into both trees.
	leafKey := makeAddress("sameKey")
	value := []byte("sameValue")
	err = ref1.Insert(leafKey, value)
	qt.Assert(t, err, qt.IsNil)
	err = ref2.Insert(leafKey, value)
	qt.Assert(t, err, qt.IsNil)

	// Both trees should have the same root.
	root1 := ref1.Root()
	root2 := ref2.Root()
	qt.Assert(t, root1, qt.IsNotNil)
	qt.Assert(t, root1, qt.DeepEquals, root2)

	// ProofByRoot should return a valid proof for the common root.
	proof, err := censusDB.ProofByRoot(root1, leafKey)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, proof, qt.Not(qt.IsNil))
}

func TestVerifyProof(t *testing.T) {
	t.Parallel()
	db := newDatabase(t)
	censusDB := NewCensusDB(db)
	censusID := uuid.New()
	ref, err := censusDB.New(censusID)
	qt.Assert(t, err, qt.IsNil)
	// Insert a key/value pair.
	leafKey := makeAddress("myKey")
	value := []byte("myValue")
	err = ref.Insert(leafKey, value)
	qt.Assert(t, err, qt.IsNil)

	// Use the new root to get a proof.
	newRoot := ref.Root()
	proof, err := censusDB.ProofByRoot(newRoot, leafKey)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, proof, qt.Not(qt.IsNil))

	// Verify the proof.
	ok := censusDB.VerifyProof(proof)
	qt.Assert(t, ok, qt.IsTrue)

	// Modify the proof and verify it again.
	proof.Value = []byte("modifiedValue")
	ok = censusDB.VerifyProof(proof)
	qt.Assert(t, ok, qt.IsFalse)
}
