package tests

import (
	"flag"
	"os"
	"time"
)

func isDebugTest() bool {
	return os.Getenv("DEBUG") != "" && os.Getenv("DEBUG") != "false"
}

func testTimeout() (time.Duration, bool) {
	f := flag.Lookup("test.timeout")
	if f == nil {
		return 0, false
	}

	d, err := time.ParseDuration(f.Value.String())
	if err != nil {
		return 0, false
	}

	return d, true
}

func maxTestTimeout() time.Duration {
	// Set up timeout based test timeout
	if timeout, hasDeadline := testTimeout(); hasDeadline {
		return timeout
	}
	// No deadline set, use a reasonable default
	if isDebugTest() {
		return 50 * time.Minute
	}
	return 20 * time.Minute
}
