package helpers

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func IsDebugTest() bool {
	return os.Getenv("DEBUG") != "" && os.Getenv("DEBUG") != "false"
}

func MaxTestTimeout(t *testing.T) time.Duration {
	t.Helper()

	// Set up timeout based on context deadline
	if deadline, hasDeadline := t.Deadline(); hasDeadline {
		// If context has a deadline, set timeout to 15 seconds before it
		// to allow for clean shutdown and error reporting
		remainingTime := time.Until(deadline)
		timeoutBuffer := 15 * time.Second

		// If we have less than the buffer time left, use half of the remaining time
		if remainingTime <= timeoutBuffer {
			timeoutBuffer = remainingTime / 2
		}

		effectiveTimeout := remainingTime - timeoutBuffer
		return effectiveTimeout
	}
	// No deadline set, use a reasonable default
	if IsDebugTest() {
		return 50 * time.Minute
	}
	return 20 * time.Minute
}

func WaitUntilCondition(ctx context.Context, interval time.Duration, condition func() bool) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for condition")
		case <-ticker.C:
			if condition() {
				return nil
			}
		}
	}
}
