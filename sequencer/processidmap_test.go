package sequencer

import (
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
)

func TestProcessIDMap(t *testing.T) {
	c := qt.New(t)

	// Create a new ProcessIDMap
	pidMap := NewProcessIDMap()

	// Test Add and Exists
	pid1 := []byte{1, 2, 3, 4}
	pid2 := []byte{5, 6, 7, 8}

	// Test adding process IDs
	c.Assert(pidMap.Add(pid1), qt.Equals, true, qt.Commentf("Should return true when adding a new process ID"))
	c.Assert(pidMap.Exists(pid1), qt.Equals, true, qt.Commentf("Should return true for existing process ID"))
	c.Assert(pidMap.Exists(pid2), qt.Equals, false, qt.Commentf("Should return false for non-existing process ID"))

	// Test Remove
	c.Assert(pidMap.Remove(pid1), qt.Equals, true, qt.Commentf("Should return true when removing an existing process ID"))
	c.Assert(pidMap.Exists(pid1), qt.Equals, false, qt.Commentf("Should return false after removal"))

	// Test with different representations of the same process ID
	hexPid := []byte{0x01, 0x02, 0x03, 0x04}
	decPid := []byte{1, 2, 3, 4}

	pidMap.Add(hexPid)
	c.Assert(pidMap.Exists(decPid), qt.Equals, true, qt.Commentf("Should return true for same process ID in different representations"))

	// Test ForEach
	pid3 := []byte{9, 10, 11, 12}
	pidMap.Add(pid2)
	pidMap.Add(pid3)

	count := 0
	pidMap.ForEach(func(pid []byte, _ time.Time) bool {
		count++
		return true
	})
	c.Assert(count, qt.Equals, 3, qt.Commentf("ForEach should iterate over all process IDs"))

	// Test early termination in ForEach
	count = 0
	pidMap.ForEach(func(pid []byte, _ time.Time) bool {
		count++
		return count < 2 // Stop after processing 2 items
	})
	c.Assert(count, qt.Equals, 2, qt.Commentf("ForEach should respect the return value"))

	// Test Len
	c.Assert(pidMap.Len(), qt.Equals, 3, qt.Commentf("Len should return the correct number of process IDs"))

	// Test List
	pids := pidMap.List()
	c.Assert(len(pids), qt.Equals, 3, qt.Commentf("List should return all process IDs"))
}
