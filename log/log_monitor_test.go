package log_test

import (
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/log"
)

// TestLogMonitorPanicOnError tests that the PanicOnErrorHook correctly panics when log.Error is called
func TestLogMonitorPanicOnError(t *testing.T) {
	c := qt.New(t)

	// Test that the hook panics on Error level logs
	c.Run("panic on log.Error", func(c *qt.C) {
		log.Error("this should not panic before installing hook")

		// Install the panic hook
		previousLogger := log.EnablePanicOnError(c.Name())
		defer log.RestoreLogger(previousLogger)

		// This should panic
		c.Assert(func() {
			log.Error("test error message")
		}, qt.PanicMatches, `ERROR found in logs during test TestLogMonitorPanicOnError/panic_on_log\.Error: test error message`)
	})

	// Test that the hook panics on Errorw level logs
	c.Run("panic on log.Errorw", func(c *qt.C) {
		// Install the panic hook
		previousLogger := log.EnablePanicOnError(c.Name())
		defer log.RestoreLogger(previousLogger)

		// This should panic
		c.Assert(func() {
			log.Errorw(nil, "test errorw message")
		}, qt.PanicMatches, `ERROR found in logs during test TestLogMonitorPanicOnError/panic_on_log\.Errorw: test errorw message`)
	})

	// Test that the hook does NOT panic on lower level logs
	c.Run("no panic on log.Warn", func(c *qt.C) {
		// Install the panic hook
		previousLogger := log.EnablePanicOnError(c.Name())
		defer log.RestoreLogger(previousLogger)

		// This should NOT panic - test by executing without expecting a panic
		log.Warn("test warning message")
		log.Info("test info message")
		log.Debug("test debug message")
		// If we reach here, no panic occurred, which is what we want
	})

	// Test that logger is properly restored
	c.Run("logger restoration", func(c *qt.C) {
		// Install and restore the panic hook
		previousLogger := log.EnablePanicOnError(c.Name())
		log.RestoreLogger(previousLogger)

		// After restoration, error logs should not panic - test by executing
		log.Error("this should not panic after restoration")
		// If we reach here, no panic occurred, which means restoration worked
	})
}
