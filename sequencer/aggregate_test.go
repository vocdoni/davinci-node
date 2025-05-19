package sequencer

import (
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
)

// TestBatchTimingBehavior tests the core behavior of our timing update:
// - Time window only starts counting when there's at least one ballot
// - Timer is reset after processing a batch
func TestBatchTimingBehavior(t *testing.T) {
	// This test verifies the logic flow, not actual dependencies
	c := qt.New(t)

	// Create a ProcessIDMap for testing
	pmap := NewProcessIDMap()
	pid := []byte{1, 2, 3, 4}

	// 1. Initially, there should be no first ballot timestamp
	_, exists := pmap.GetFirstBallotTime(pid)
	c.Assert(exists, qt.Equals, false, qt.Commentf("Initially, there should be no first ballot timestamp"))

	// Set the first ballot time
	startTime := time.Now()
	pmap.SetFirstBallotTime(pid)
	initialTime, exists := pmap.GetFirstBallotTime(pid)
	c.Assert(exists, qt.Equals, true, qt.Commentf("After setting, first ballot timestamp should exist"))

	// Check time is recent (within 1 second of now)
	c.Assert(initialTime.After(startTime.Add(-time.Second)), qt.IsTrue,
		qt.Commentf("First ballot time should be recent"))

	// Sleep a bit to ensure time difference
	time.Sleep(10 * time.Millisecond)

	// 3. After processing a batch, the timestamp should be cleared
	pmap.ClearFirstBallotTime(pid)
	_, exists = pmap.GetFirstBallotTime(pid)
	c.Assert(exists, qt.Equals, false, qt.Commentf("After clearing, timestamp should not exist"))

	// 4. When next ballot arrives, a new timestamp should be set
	newStartTime := time.Now()
	pmap.SetFirstBallotTime(pid)
	newTime, exists := pmap.GetFirstBallotTime(pid)
	c.Assert(exists, qt.Equals, true, qt.Commentf("After setting again, timestamp should exist"))
	c.Assert(newTime.After(newStartTime.Add(-time.Second)), qt.IsTrue,
		qt.Commentf("New first ballot time should be recent"))
}
