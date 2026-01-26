package log_test

import (
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/log"
)

// TestLogMonitorPanicOnError tests that the PanicOnErrorHook correctly panics when log.Error is called
func TestLogMonitorPanicOnError(t *testing.T) {
	c := qt.New(t)

	// Test that the hook panics on Error level logs
	c.Run("panic on log.Error", func(c *qt.C) {
		log.Error("this should not panic before installing hook")

		ch := make(chan string, 1)
		previousLogger := log.EnablePanicOnErrorWithHandler(c.Name(), 100*time.Millisecond, func(msg string) {
			ch <- msg
		})
		defer log.RestoreLogger(previousLogger)

		log.Error("test error message")

		select {
		case got := <-ch:
			c.Assert(got, qt.Matches, `ERROR found in logs during test TestLogMonitorPanicOnError/panic_on_log\.Error: test error message`)
		case <-time.After(500 * time.Millisecond):
			c.Fatalf("expected delayed panic handler to fire")
		}
	})

	// Test that the hook panics on Errorw level logs
	c.Run("panic on log.Errorw", func(c *qt.C) {
		ch := make(chan string, 1)
		previousLogger := log.EnablePanicOnErrorWithHandler(c.Name(), 100*time.Millisecond, func(msg string) {
			ch <- msg
		})
		defer log.RestoreLogger(previousLogger)

		log.Errorw(nil, "test errorw message")

		select {
		case got := <-ch:
			c.Assert(got, qt.Matches, `ERROR found in logs during test TestLogMonitorPanicOnError/panic_on_log\.Errorw: test errorw message`)
		case <-time.After(500 * time.Millisecond):
			c.Fatalf("expected delayed panic handler to fire")
		}
	})

	// Test that the hook does NOT panic on lower level logs
	c.Run("no panic on log.Warn", func(c *qt.C) {
		ch := make(chan string, 1)
		previousLogger := log.EnablePanicOnErrorWithHandler(c.Name(), 100*time.Millisecond, func(msg string) {
			ch <- msg
		})
		defer log.RestoreLogger(previousLogger)

		log.Warn("test warning message")
		log.Info("test info message")
		log.Debug("test debug message")

		select {
		case got := <-ch:
			c.Fatalf("unexpected panic handler call: %s", got)
		case <-time.After(200 * time.Millisecond):
		}
	})

	// Test that logger is properly restored
	c.Run("logger restoration", func(c *qt.C) {
		ch := make(chan string, 1)
		previousLogger := log.EnablePanicOnErrorWithHandler(c.Name(), 100*time.Millisecond, func(msg string) {
			ch <- msg
		})
		log.RestoreLogger(previousLogger)

		log.Error("this should not panic after restoration")

		select {
		case got := <-ch:
			c.Fatalf("unexpected panic handler call after restoration: %s", got)
		case <-time.After(200 * time.Millisecond):
		}
	})
}
