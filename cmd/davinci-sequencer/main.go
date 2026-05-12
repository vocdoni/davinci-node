package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/metadata"
	"github.com/vocdoni/davinci-node/sequencer"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/web3"
	"github.com/vocdoni/davinci-node/web3/txmanager"
	"github.com/vocdoni/davinci-node/workers"
)

// Services holds all the running services
type Services struct {
	TxManagers       []*txmanager.TxManager
	Storage          *storage.Storage
	StateSync        *service.StateSync
	CensusDownloader *service.CensusDownloader
	ProcessMons      []*service.ProcessMonitor
	API              *service.APIService
	Sequencer        *service.SequencerService
}

func main() {
	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logging
	log.Init(cfg.Log.Level, cfg.Log.Output, nil)
	log.Infow("starting davinci-sequencer", "version", Version)

	// Check for worker mode from --worker flag
	if cfg.Worker.SequencerURL != "" {
		runWorkerMode(cfg)
		return
	}

	// Master mode
	// Validate configuration
	if err := validateConfig(cfg); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Create context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup services
	services, err := setupServices(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to setup services: %v", err)
	}
	defer shutdownServices(services)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	sig := <-sigCh
	log.Infow("received signal, shutting down", "signal", sig.String())
}

// runWorkerMode runs the sequencer in worker mode
func runWorkerMode(cfg *Config) {
	log.Infow("starting in worker mode", "master", cfg.Worker.SequencerURL)

	// Check if a worker address is provided
	if cfg.Worker.Address == "" {
		log.Fatalf("valid worker address is required (use --worker.address flag)")
	}
	// Check if a worker signature is provided
	if cfg.Worker.Authtoken == "" {
		log.Fatalf("valid worker authtoken is required (use --worker.authtoken flag)")
	}
	// Check if a worker sequencer URL is provided
	if cfg.Worker.SequencerURL == "" {
		log.Fatalf("valid worker sequencer URL is required (use --worker.sequencerURL flag)")
	}

	// Initialize storage database (only for local process tracking)
	log.Infow("initializing storage", "datadir", cfg.Datadir, "type", db.TypePebble)
	storagedb, err := metadb.New(db.TypePebble, cfg.Datadir)
	if err != nil {
		log.Fatalf("failed to initialize storage: %v", err)
	}
	storage := storage.New(storagedb)
	defer storage.Close()

	// Download circuit artifacts
	artifactsDir := path.Join(cfg.Datadir, "artifacts")
	artifactsCtx, cancel := context.WithTimeout(context.Background(), artifactsTimeout)
	defer cancel()
	log.Infow("preparing zkSNARK circuit worker artifacts", "timeout", artifactsTimeout, "artifactsDir", artifactsDir)
	if err := service.DownloadWorkerArtifacts(artifactsCtx, artifactsDir); err != nil {
		log.Fatalf("failed to download artifacts: %v", err)
	}

	// Create worker sequencer
	workerSeq, err := sequencer.NewWorker(
		storage,
		cfg.Worker.SequencerURL,
		cfg.Worker.Address,
		cfg.Worker.Authtoken,
		cfg.Worker.Name,
	)
	if err != nil {
		log.Fatalf("failed to create worker: %v", err)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start worker
	if err := workerSeq.Start(ctx); err != nil {
		log.Fatalf("failed to start worker: %v", err)
	}

	log.Infow("worker is running", "address", cfg.Worker.Address)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	sig := <-sigCh
	log.Infow("received signal, shutting down worker", "signal", sig.String())

	if err := workerSeq.Stop(); err != nil {
		log.Warnw("failed to stop worker cleanly", "error", err)
	}
}

// setupServices initializes and starts all required services
func setupServices(ctx context.Context, cfg *Config) (services *Services, err error) {
	services = &Services{}
	defer func() {
		if err != nil {
			shutdownServices(services)
		}
	}()

	// Download circuit artifacts
	artifactsDir := path.Join(cfg.Datadir, "artifacts")
	artifactsCtx, cancel := context.WithTimeout(context.Background(), artifactsTimeout)
	defer cancel()
	log.Infow("preparing zkSNARK circuit full sequencer artifacts", "timeout", artifactsTimeout, "artifactsDir", artifactsDir)
	if err := service.DownloadArtifacts(artifactsCtx, artifactsDir); err != nil {
		return nil, fmt.Errorf("failed to download artifacts: %w", err)
	}

	// Initialize storage database
	log.Infow("initializing storage", "datadir", cfg.Datadir, "type", db.TypePebble)
	storagedb, err := metadb.New(db.TypePebble, cfg.Datadir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}
	services.Storage = storage.New(storagedb)

	// Force cleanup if requested
	if cfg.ForceCleanup {
		log.Warn("force cleanup enabled: cleaning all pending verified votes, aggregated batches and state transitions")
		if err := services.Storage.CleanAllPending(); err != nil {
			return nil, fmt.Errorf("failed to clean all pending items: %w", err)
		}
		log.Info("force cleanup completed successfully")
	}

	runtimes, err := cfg.Web3.InitRuntimes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize runtimes: %w", err)
	}
	for _, runtime := range runtimes {
		log.Infow("web3 runtime initialized",
			"chainID", runtime.ChainID,
			"account", runtime.Contracts.AccountAddress().Hex(),
			"gasMultiplier", runtime.Contracts.GasMultiplier,
			"availableEndpoints", runtime.AvailableEndpoints,
			"processRegistry", runtime.Contracts.ContractsAddresses.ProcessRegistry.Hex(),
			"consensusAPI", runtime.Contracts.Web3ConsensusAPIEndpoint,
		)
	}

	log.Infow("initializing web3 runtimes", "numNetworks", len(runtimes))
	runtimeRouter, err := web3.NewRuntimeRouter(runtimes...)
	if err != nil {
		return nil, fmt.Errorf("failed to create runtime router: %w", err)
	}

	// Start census downloader
	log.Info("starting census downloader")
	services.CensusDownloader = service.NewCensusDownloader(runtimeRouter, services.Storage, service.DefaultCensusDownloaderConfig)
	if err := services.CensusDownloader.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start census downloader: %w", err)
	}

	// Start StateSync
	services.StateSync = service.NewStateSync(runtimeRouter, services.Storage)
	if err := services.StateSync.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start state sync: %v", err)
	}

	// Start process monitors and transaction managers
	services.TxManagers = make([]*txmanager.TxManager, 0, len(runtimes))
	services.ProcessMons = make([]*service.ProcessMonitor, 0, len(runtimes))
	for _, runtime := range runtimes {
		log.Infow("starting process monitor", "chainID", runtime.ChainID)
		processMon := service.NewProcessMonitor(
			runtime.Contracts,
			runtime.ProcessIDVersion,
			services.Storage,
			services.CensusDownloader,
			services.StateSync,
			monitorInterval,
		)
		if err := processMon.Start(ctx); err != nil {
			return nil, fmt.Errorf("failed to start process monitor for %d: %w", runtime.ChainID, err)
		}
		services.TxManagers = append(services.TxManagers, runtime.TxManager)
		services.ProcessMons = append(services.ProcessMons, processMon)
	}

	log.Infow("starting API service", "host", cfg.API.Host, "port", cfg.API.Port)
	services.API = service.NewAPI(
		services.Storage,
		cfg.API.Host,
		cfg.API.Port,
		runtimeRouter,
		metadata.PinataMetadataProviderConfig{
			HostnameURL:  cfg.Metadata.PinataHostnameURL,
			HostnameJWT:  cfg.Metadata.PinataHostnameJWT,
			GatewayURL:   cfg.Metadata.PinataGatewayURL,
			GatewayToken: cfg.Metadata.PinataGatewayToken,
		},
		cfg.Log.DisableAPI)

	// Configure worker API if enabled
	if cfg.API.SequencerWorkersSeed != "" {
		services.API.SetWorkerConfig(
			cfg.API.SequencerWorkersSeed,
			cfg.API.WorkersAuthtokenExpiration,
			cfg.Worker.Timeout,
			&workers.WorkerBanRules{
				BanTimeout:          cfg.API.WorkersBanTimeout,
				FailuresToGetBanned: cfg.API.WorkersFailuresToGetBanned,
			},
		)
	}

	// Start API service
	if err := services.API.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start API service: %w", err)
	}

	// Start sequencer service
	log.Infow("starting sequencer service", "batchTimeWindow", cfg.Batch.Time.String())
	services.Sequencer = service.NewSequencer(services.Storage, runtimeRouter, cfg.Batch.Time, services.API.API)
	if err := services.Sequencer.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start sequencer service: %w", err)
	}

	log.Info("davinci-node is running, ready to process votes!")
	return services, nil
}

// shutdownServices gracefully shuts down all services
func shutdownServices(services *Services) {
	if services == nil {
		return
	}
	// Stop services in reverse order of startup
	if services.Sequencer != nil {
		services.Sequencer.Stop()
	}
	if services.API != nil {
		services.API.Stop()
	}
	for _, processMon := range services.ProcessMons {
		if processMon != nil {
			processMon.Stop()
		}
	}
	if services.StateSync != nil {
		services.StateSync.Stop()
	}
	if services.CensusDownloader != nil {
		services.CensusDownloader.Stop()
	}
	for _, txManager := range services.TxManagers {
		if txManager != nil {
			txManager.Stop()
		}
	}
	if services.Storage != nil {
		services.Storage.Close()
	}
}
