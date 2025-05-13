package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/metadb"
)

func TestFinalizerService(t *testing.T) {
	c := qt.New(t)

	// Setup storage
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")
	database, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	store := storage.New(database)
	defer store.Close()

	// Create finalizer service with a short monitor interval for testing
	finService := NewFinalizer(store, store.StateDB(), 200*time.Millisecond)

	// Start the service in background
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = finService.Start(ctx, time.Minute)
	c.Assert(err, qt.IsNil)
	defer finService.Stop()

	// Test that starting an already running service returns an error
	err = finService.Start(ctx, time.Minute)
	c.Assert(err, qt.ErrorMatches, "service already running")

	// Test stopping and restarting the service
	finService.Stop()
	err = finService.Start(ctx, time.Minute)
	c.Assert(err, qt.IsNil)

	// Verify we can use the underlying finalizer directly
	pid := &types.ProcessID{}
	go func() {
		// Send a process to finalize
		finService.OndemandCh <- pid
	}()
}
