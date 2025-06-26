package storage

import (
	"path/filepath"
	"testing"

	qt "github.com/frankban/quicktest"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/metadb"
)

func TestIncreaseWorkerJobCount(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	testDB, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	storage := New(testDB)
	defer storage.Close()

	address := "0x1234567890abcdef"

	// Test initial count
	success, failed, err := storage.WorkerJobCount(address)
	c.Assert(err, qt.IsNil)
	c.Assert(success, qt.Equals, int64(0))
	c.Assert(failed, qt.Equals, int64(0))

	// Increase success count
	err = storage.IncreaseWorkerJobCount(address, 5)
	c.Assert(err, qt.IsNil)

	// Check updated count
	success, failed, err = storage.WorkerJobCount(address)
	c.Assert(err, qt.IsNil)
	c.Assert(success, qt.Equals, int64(5))
	c.Assert(failed, qt.Equals, int64(0))

	// Increase again
	err = storage.IncreaseWorkerJobCount(address, 3)
	c.Assert(err, qt.IsNil)

	success, failed, err = storage.WorkerJobCount(address)
	c.Assert(err, qt.IsNil)
	c.Assert(success, qt.Equals, int64(8))
	c.Assert(failed, qt.Equals, int64(0))
}

func TestIncreaseWorkerFailedJobCount(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	testDB, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	storage := New(testDB)
	defer storage.Close()

	address := "0x1234567890abcdef"

	// Increase failed count
	err = storage.IncreaseWorkerFailedJobCount(address, 2)
	c.Assert(err, qt.IsNil)

	// Check updated count
	success, failed, err := storage.WorkerJobCount(address)
	c.Assert(err, qt.IsNil)
	c.Assert(success, qt.Equals, int64(0))
	c.Assert(failed, qt.Equals, int64(2))

	// Increase again
	err = storage.IncreaseWorkerFailedJobCount(address, 1)
	c.Assert(err, qt.IsNil)

	success, failed, err = storage.WorkerJobCount(address)
	c.Assert(err, qt.IsNil)
	c.Assert(success, qt.Equals, int64(0))
	c.Assert(failed, qt.Equals, int64(3))
}

func TestListWorkerJobCount(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	testDB, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	storage := New(testDB)
	defer storage.Close()

	addresses := []string{
		"0x1111111111111111",
		"0x2222222222222222",
		"0x3333333333333333",
	}

	// Add stats for multiple workers
	err = storage.IncreaseWorkerJobCount(addresses[0], 10)
	c.Assert(err, qt.IsNil)
	err = storage.IncreaseWorkerFailedJobCount(addresses[0], 2)
	c.Assert(err, qt.IsNil)

	err = storage.IncreaseWorkerJobCount(addresses[1], 5)
	c.Assert(err, qt.IsNil)

	err = storage.IncreaseWorkerFailedJobCount(addresses[2], 1)
	c.Assert(err, qt.IsNil)

	// List all workers
	workerStats, err := storage.ListWorkerJobCount()
	c.Assert(err, qt.IsNil)

	// Check results
	expectedStats := map[string][2]int64{
		addresses[0]: {10, 2},
		addresses[1]: {5, 0},
		addresses[2]: {0, 1},
	}

	c.Assert(len(workerStats), qt.Equals, len(expectedStats))

	for addr, expected := range expectedStats {
		actual, exists := workerStats[addr]
		c.Assert(exists, qt.IsTrue, qt.Commentf("Worker %s not found in results", addr))
		c.Assert(actual[0], qt.Equals, expected[0], qt.Commentf("Worker %s success count", addr))
		c.Assert(actual[1], qt.Equals, expected[1], qt.Commentf("Worker %s failed count", addr))
	}
}

func TestWorkerJobCountNonExistent(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	testDB, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	storage := New(testDB)
	defer storage.Close()

	// Check non-existent worker
	success, failed, err := storage.WorkerJobCount("0xnonexistent")
	c.Assert(err, qt.IsNil)
	c.Assert(success, qt.Equals, int64(0))
	c.Assert(failed, qt.Equals, int64(0))
}

func TestWorkerStatsConcurrency(t *testing.T) {
	c := qt.New(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	testDB, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	storage := New(testDB)
	defer storage.Close()

	address := "0x1234567890abcdef"
	numGoroutines := 10
	incrementsPerGoroutine := 100

	// Run concurrent increments
	done := make(chan struct{}, numGoroutines)
	for range numGoroutines {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < incrementsPerGoroutine; j++ {
				if j%2 == 0 {
					_ = storage.IncreaseWorkerJobCount(address, 1)
				} else {
					_ = storage.IncreaseWorkerFailedJobCount(address, 1)
				}
			}
		}()
	}

	// Wait for all goroutines to complete
	for range numGoroutines {
		<-done
	}

	// Check final counts
	success, failed, err := storage.WorkerJobCount(address)
	c.Assert(err, qt.IsNil)

	expectedTotal := int64(numGoroutines * incrementsPerGoroutine)
	actualTotal := success + failed

	c.Assert(actualTotal, qt.Equals, expectedTotal,
		qt.Commentf("Expected total count %d, got %d (success=%d, failed=%d)",
			expectedTotal, actualTotal, success, failed))

	// The exact split between success and failed may vary due to concurrency,
	// but the total should be correct
	c.Assert(success >= 0, qt.IsTrue, qt.Commentf("Success count should be non-negative"))
	c.Assert(failed >= 0, qt.IsTrue, qt.Commentf("Failed count should be non-negative"))
}
