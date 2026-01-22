package storage

import (
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/spec/params"
)

// TestHasVerifiedResults tests the HasVerifiedResults functionality
func TestHasVerifiedResults(t *testing.T) {
	c := qt.New(t)

	// Create a test storage instance
	dbDir := t.TempDir()
	testdb, err := metadb.New(db.TypePebble, dbDir)
	c.Assert(err, qt.IsNil)

	// Create storage instance
	stg := New(testdb)
	defer stg.Close()

	// Test process ID
	processID := testutil.RandomProcessID()

	// Test 1: Check non-existent results
	hasResults := stg.HasVerifiedResults(processID)
	c.Assert(hasResults, qt.IsFalse, qt.Commentf("expected no results for new process"))

	// Test 2: Push verified results
	results := &VerifiedResults{
		ProcessID: processID,
		Inputs: ResultsVerifierProofInputs{
			StateRoot: testutil.StateRoot().MathBigInt(),
			Results: [params.FieldsPerBallot]*big.Int{
				big.NewInt(100),
				big.NewInt(200),
				big.NewInt(300),
				big.NewInt(400),
			},
		},
		// Proof would be set in real usage, but not needed for this test
	}
	err = stg.PushVerifiedResults(results)
	c.Assert(err, qt.IsNil)

	// Test 3: Check that results now exist
	hasResults = stg.HasVerifiedResults(processID)
	c.Assert(hasResults, qt.IsTrue, qt.Commentf("expected results to exist after push"))

	// Test 4: Check NextVerifiedResults still returns the results
	nextResults, err := stg.NextVerifiedResults()
	c.Assert(err, qt.IsNil)
	c.Assert(nextResults.ProcessID, qt.DeepEquals, processID)

	// Test 5: Mark results as done
	err = stg.MarkVerifiedResultsDone(processID)
	c.Assert(err, qt.IsNil)

	// Test 6: Check that results no longer exist after marking done
	hasResults = stg.HasVerifiedResults(processID)
	c.Assert(hasResults, qt.IsFalse, qt.Commentf("expected no results after marking done"))

	// Test 7: Try pushing duplicate results (should fail)
	err = stg.PushVerifiedResults(results)
	c.Assert(err, qt.IsNil) // First push after deletion should succeed

	err = stg.PushVerifiedResults(results)
	c.Assert(err, qt.Not(qt.IsNil), qt.Commentf("expected error when pushing duplicate results"))
	c.Assert(err.Error(), qt.Contains, "already exists")
}

// TestHasVerifiedResultsConcurrency tests concurrent access to HasVerifiedResults
func TestHasVerifiedResultsConcurrency(t *testing.T) {
	c := qt.New(t)

	// Create a test storage instance
	dbDir := t.TempDir()
	testdb, err := metadb.New(db.TypePebble, dbDir)
	c.Assert(err, qt.IsNil)

	// Create storage instance
	stg := New(testdb)
	defer stg.Close()

	// Test with multiple processes
	numProcesses := 10
	numGoroutines := 5

	// Create and push results for multiple processes
	for i := range numProcesses {
		processID := testutil.DeterministicProcessID(uint64(i))
		results := &VerifiedResults{
			ProcessID: processID,
			Inputs: ResultsVerifierProofInputs{
				StateRoot: testutil.RandomStateRoot().MathBigInt(),
				Results: [params.FieldsPerBallot]*big.Int{
					big.NewInt(int64(i * 100)),
					big.NewInt(int64(i * 200)),
					big.NewInt(int64(i * 300)),
					big.NewInt(int64(i * 400)),
				},
			},
		}
		err := stg.PushVerifiedResults(results)
		c.Assert(err, qt.IsNil)
	}

	// Concurrently check for results
	done := make(chan bool)
	for i := range numGoroutines {
		go func(workerID int) {
			defer func() { done <- true }()

			for j := range numProcesses {
				processID := testutil.DeterministicProcessID(uint64(j))
				if !stg.HasVerifiedResults(processID) {
					t.Errorf("worker %d: expected results for process %d", workerID, j)
					return
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for range numGoroutines {
		<-done
	}

	// Verify all results still exist
	for i := range numProcesses {
		processID := testutil.DeterministicProcessID(uint64(i))
		hasResults := stg.HasVerifiedResults(processID)
		c.Assert(hasResults, qt.IsTrue, qt.Commentf("expected results for process %d", i))
	}
}
