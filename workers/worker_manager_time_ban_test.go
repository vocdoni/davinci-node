package workers

import (
	"sync/atomic"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
)

func TestWorkerTimeBanCoverage(t *testing.T) {
	c := qt.New(t)

	rules := &WorkerBanRules{
		BanTimeout:          3 * time.Minute,
		FailuresToGetBanned: 3,
	}

	t.Run("Time-based banning scenarios", func(t *testing.T) {
		worker := &Worker{
			Address:          "test-worker",
			consecutiveFails: 0, // No consecutive fails
		}

		// Test worker with no ban time set (bannedUntilNanos = 0)
		c.Assert(worker.IsBanned(rules), qt.IsFalse)

		// Test worker with future ban time (still banned)
		futureTime := time.Now().Add(1 * time.Hour)
		worker.SetBannedUntil(futureTime)
		c.Assert(worker.IsBanned(rules), qt.IsTrue)

		// Test worker with past ban time (ban expired)
		pastTime := time.Now().Add(-1 * time.Hour)
		worker.SetBannedUntil(pastTime)
		c.Assert(worker.IsBanned(rules), qt.IsFalse)

		// Test worker with ban time very close to now (edge case)
		almostNow := time.Now().Add(1 * time.Millisecond)
		worker.SetBannedUntil(almostNow)
		c.Assert(worker.IsBanned(rules), qt.IsTrue)

		// Wait for the ban to expire and test again
		time.Sleep(2 * time.Millisecond)
		c.Assert(worker.IsBanned(rules), qt.IsFalse)
	})

	t.Run("Combined consecutive fails and time-based banning", func(t *testing.T) {
		// Test worker that is banned by consecutive fails AND has a time-based ban
		worker := &Worker{
			Address:          "test-worker",
			consecutiveFails: 5, // Above threshold
		}

		// Should be banned due to consecutive fails, regardless of time ban
		c.Assert(worker.IsBanned(rules), qt.IsTrue)

		// Set a future ban time - should still be banned
		futureTime := time.Now().Add(1 * time.Hour)
		worker.SetBannedUntil(futureTime)
		c.Assert(worker.IsBanned(rules), qt.IsTrue)

		// Set a past ban time - should still be banned due to consecutive fails
		pastTime := time.Now().Add(-1 * time.Hour)
		worker.SetBannedUntil(pastTime)
		c.Assert(worker.IsBanned(rules), qt.IsTrue)

		// Reset consecutive fails but keep future ban time
		atomic.StoreInt64(&worker.consecutiveFails, 0)
		worker.SetBannedUntil(time.Now().Add(1 * time.Hour))
		c.Assert(worker.IsBanned(rules), qt.IsTrue) // Still banned due to time

		// Clear ban time - should not be banned
		worker.SetBannedUntil(time.Time{})
		c.Assert(worker.IsBanned(rules), qt.IsFalse)
	})

	t.Run("Edge cases for time comparison", func(t *testing.T) {
		worker := &Worker{
			Address:          "test-worker",
			consecutiveFails: 0,
		}

		// Test with zero time (should not be banned)
		worker.SetBannedUntil(time.Time{})
		c.Assert(worker.IsBanned(rules), qt.IsFalse)

		// Test with exactly current time (should be banned by a tiny margin)
		now := time.Now()
		worker.SetBannedUntil(now)
		// This might be banned or not depending on timing, but it exercises the code path
		_ = worker.IsBanned(rules)

		// Test with time well in the future (more reliable than nanoseconds)
		futureMicro := time.Now().Add(100 * time.Microsecond)
		worker.SetBannedUntil(futureMicro)
		c.Assert(worker.IsBanned(rules), qt.IsTrue)

		// Test with time well in the past
		pastMicro := time.Now().Add(-100 * time.Microsecond)
		worker.SetBannedUntil(pastMicro)
		c.Assert(worker.IsBanned(rules), qt.IsFalse)

		// Test the specific code path where bannedUntil != 0 but time has passed
		// This ensures we cover the `return time.Now().UnixNano() < bannedUntil` line
		pastTime := time.Now().Add(-1 * time.Second)
		worker.SetBannedUntil(pastTime)
		banned := worker.IsBanned(rules)
		c.Assert(banned, qt.IsFalse) // Should not be banned since time has passed
	})
}
