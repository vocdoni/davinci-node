package service

import (
	"context"
	"fmt"
	"time"

	"github.com/vocdoni/vocdoni-z-sandbox/finalizer"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"go.vocdoni.io/dvote/db"
)

// FinalizerService represents a service that handles the finalization of voting processes
// based on their end time or on-demand.
type FinalizerService struct {
	*finalizer.Finalizer
	cancel context.CancelFunc
}

// NewFinalizer creates a new finalizer service instance.
// The monitorInterval parameter specifies how often the service should check for processes to finalize.
// If monitorInterval is 0, periodic monitoring is disabled and processes will only be finalized on-demand.
func NewFinalizer(stg *storage.Storage, stateDB db.Database, monitorInterval time.Duration) *FinalizerService {
	return &FinalizerService{
		Finalizer: finalizer.New(stg, stateDB),
	}
}

// Start begins the finalizer service. It returns an error if the service
// is already running or if it fails to start.
func (fs *FinalizerService) Start(ctx context.Context, interval time.Duration) error {
	if fs.cancel != nil {
		return fmt.Errorf("service already running")
	}

	ctx, cancel := context.WithCancel(ctx)
	fs.cancel = cancel

	// Start the underlying finalizer
	fs.Finalizer.Start(ctx, interval)

	log.Infow("finalizer service started")
	return nil
}

// Stop halts the finalizer service.
func (fs *FinalizerService) Stop() {
	if fs.cancel != nil {
		fs.cancel()
		fs.cancel = nil

		// Call the Close method on the Finalizer to ensure all goroutines exit
		// before resources like the database are closed
		if fs.Finalizer != nil {
			fs.Close()
		}

		log.Infow("finalizer service stopped")
	}
}
