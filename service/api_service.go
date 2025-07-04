package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
)

// APIService represents a service that manages the HTTP API server.
type APIService struct {
	storage       *storage.Storage
	API           *api.API
	mu            sync.Mutex
	cancel        context.CancelFunc
	host          string
	port          int
	network       string
	workerUrlSeed string
	workerTimeout time.Duration
	banRules      *api.BanRules // Custom ban rules for workers
}

// NewAPI creates a new APIService instance.
func NewAPI(storage *storage.Storage, host string, port int, network string, disableLogging bool) *APIService {
	if disableLogging {
		api.DisabledLogging = disableLogging
		log.Debugw("API logging is disabled")
	}
	return &APIService{
		storage: storage,
		host:    host,
		port:    port,
		network: network,
	}
}

// SetWorkerConfig configures the worker settings for the API service.
func (as *APIService) SetWorkerConfig(urlSeed string, timeout time.Duration, banRules *api.BanRules) {
	log.Debugw("Setting worker configuration",
		"urlSeed", urlSeed,
		"timeout", timeout)
	as.mu.Lock()
	defer as.mu.Unlock()
	as.workerUrlSeed = urlSeed
	as.workerTimeout = timeout
	as.banRules = banRules
}

// Start begins the API server. It returns an error if the service
// is already running or if it fails to start.
func (as *APIService) Start(ctx context.Context) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	if as.cancel != nil {
		return fmt.Errorf("service already running")
	}

	_, as.cancel = context.WithCancel(ctx)

	// Create API instance with existing storage
	var err error
	as.API, err = api.New(ctx, &api.APIConfig{
		Host:          as.host,
		Port:          as.port,
		Storage:       as.storage,
		Network:       as.network,
		WorkerUrlSeed: as.workerUrlSeed,
		WorkerTimeout: as.workerTimeout,
		BanRules:      as.banRules,
	})
	if err != nil {
		as.cancel = nil
		return fmt.Errorf("failed to start API server: %w", err)
	}

	return nil
}

// Stop halts the API server.
func (as *APIService) Stop() {
	as.mu.Lock()
	defer as.mu.Unlock()

	if as.cancel != nil {
		as.cancel()
		as.cancel = nil
	}
}

// HostPort returns the host and port of the API server.
func (as *APIService) HostPort() (string, int) {
	return as.host, as.port
}
