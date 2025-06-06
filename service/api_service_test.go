package service

import (
	"context"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
)

func TestAPIService(t *testing.T) {
	c := qt.New(t)

	// Setup storage
	kv := memdb.New()
	store := storage.New(kv)
	defer store.Close()

	// Create API service with a random available port
	apiService := NewAPI(store, "127.0.0.1", 0, "local", false) // Port 0 lets the OS choose an available port

	// Start service in background
	ctx := context.Background()

	err := apiService.Start(ctx)
	c.Assert(err, qt.IsNil)
	defer apiService.Stop()

	// Give the service time to start
	time.Sleep(2 * time.Second)

	// Test stopping and restarting
	apiService.Stop()
	err = apiService.Start(ctx)
	c.Assert(err, qt.IsNil)

	// Test starting an already running service
	err = apiService.Start(ctx)
	c.Assert(err, qt.ErrorMatches, "service already running")
}
