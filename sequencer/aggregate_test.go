package sequencer

import (
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/internal/testutil"
)

// TestBatchTimingBehavior tests the core behavior of our timing update:
// - Time window only starts counting when there's at least one ballot
// - Timer is reset after processing a batch
func TestBatchTimingBehavior(t *testing.T) {
	// This test verifies the logic flow, not actual dependencies
	c := qt.New(t)

	// Create a ProcessIDMap for testing
	pmap := NewProcessIDMap()
	processID := testutil.RandomProcessID()

	// 1. Initially, there should be no first ballot timestamp
	_, exists := pmap.GetFirstBallotTime(processID)
	c.Assert(exists, qt.Equals, false, qt.Commentf("Initially, there should be no first ballot timestamp"))

	// Set the first ballot time
	startTime := time.Now()
	pmap.SetFirstBallotTime(processID)
	initialTime, exists := pmap.GetFirstBallotTime(processID)
	c.Assert(exists, qt.Equals, true, qt.Commentf("After setting, first ballot timestamp should exist"))

	// Check time is recent (within 1 second of now)
	c.Assert(initialTime.After(startTime.Add(-time.Second)), qt.IsTrue,
		qt.Commentf("First ballot time should be recent"))

	// Sleep a bit to ensure time difference
	time.Sleep(10 * time.Millisecond)

	// 3. After processing a batch, the timestamp should be cleared
	pmap.ClearFirstBallotTime(processID)
	_, exists = pmap.GetFirstBallotTime(processID)
	c.Assert(exists, qt.Equals, false, qt.Commentf("After clearing, timestamp should not exist"))

	// 4. When next ballot arrives, a new timestamp should be set
	newStartTime := time.Now()
	pmap.SetFirstBallotTime(processID)
	newTime, exists := pmap.GetFirstBallotTime(processID)
	c.Assert(exists, qt.Equals, true, qt.Commentf("After setting again, timestamp should exist"))
	c.Assert(newTime.After(newStartTime.Add(-time.Second)), qt.IsTrue,
		qt.Commentf("New first ballot time should be recent"))
}
