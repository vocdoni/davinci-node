package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/sequencer"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/web3"
	"github.com/vocdoni/davinci-node/web3/rpc/chainlist"
	"github.com/vocdoni/davinci-node/workers"
)

// Services holds all the running services
type Services struct {
	Contracts  *web3.Contracts
	Storage    *storage.Storage
	ProcessMon *service.ProcessMonitor
	API        *service.APIService
	Sequencer  *service.SequencerService
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

	// Get contract addresses
	addresses, err := getContractAddresses(cfg)
	if err != nil {
		log.Fatalf("Failed to get contract addresses: %v", err)
	}

	// Create context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup services
	services, err := setupServices(ctx, cfg, addresses)
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
	if err := service.DownloadWorkerArtifacts(artifactsTimeout, path.Join(cfg.Datadir, "artifacts")); err != nil {
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

// getContractAddresses returns the contract addresses based on configuration
func getContractAddresses(cfg *Config) (*web3.Addresses, error) {
	// Get default contract addresses for selected network
	networkConfig, ok := config.DefaultConfig[cfg.Web3.Network]
	if !ok {
		return nil, fmt.Errorf("no configuration found for network %s", cfg.Web3.Network)
	}

	// Override with custom addresses if provided
	processRegistryAddr := networkConfig.ProcessRegistrySmartContract
	if cfg.Web3.ProcessAddr != "" {
		processRegistryAddr = cfg.Web3.ProcessAddr
	}

	orgRegistryAddr := networkConfig.OrganizationRegistrySmartContract
	if cfg.Web3.OrganizationsAddr != "" {
		orgRegistryAddr = cfg.Web3.OrganizationsAddr
	}

	// Log the contract addresses being used
	log.Infow("using contract addresses",
		"network", cfg.Web3.Network,
		"processRegistry", processRegistryAddr,
		"orgRegistry", orgRegistryAddr)

	// Create the addresses struct
	return &web3.Addresses{
		ProcessRegistry:      common.HexToAddress(processRegistryAddr),
		OrganizationRegistry: common.HexToAddress(orgRegistryAddr),
	}, nil
}

// setupServices initializes and starts all required services
func setupServices(ctx context.Context, cfg *Config, addresses *web3.Addresses) (*Services, error) {
	services := &Services{}

	// Download circuit artifacts
	if err := service.DownloadArtifacts(artifactsTimeout, path.Join(cfg.Datadir, "artifacts")); err != nil {
		return nil, fmt.Errorf("failed to download artifacts: %w", err)
	}

	// Initialize storage database
	log.Infow("initializing storage", "datadir", cfg.Datadir, "type", db.TypePebble)
	storagedb, err := metadb.New(db.TypePebble, cfg.Datadir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}
	services.Storage = storage.New(storagedb)

	// Initialize web3 contracts
	log.Info("initializing web3 contracts")

	// Default RPC endpoints by network if not provided by user
	w3rpc := cfg.Web3.Rpc
	if len(w3rpc) == 0 {
		log.Infow("no RPC endpoints provided, using chainlist.org", "network", cfg.Web3.Network)
		list, err := chainlist.ChainList()
		if err != nil {
			return nil, fmt.Errorf("failed to get chain list: %w", err)
		}
		id, ok := list[cfg.Web3.Network]
		if !ok {
			return nil, fmt.Errorf("network %s not found in chain list", cfg.Web3.Network)
		}
		endpoints, err := chainlist.EndpointList(cfg.Web3.Network, 10)
		if err != nil {
			return nil, fmt.Errorf("failed to get endpoints for network %s: %w", cfg.Web3.Network, err)
		}
		log.Infow("using endpoints from chain list", "chainID", id, "network", cfg.Web3.Network, "endpoints", endpoints)
		w3rpc = endpoints
	}

	// Initialize web3 contracts
	services.Contracts, err = web3.New(w3rpc, cfg.Web3.Capi)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize web3 client: %w", err)
	}

	// Load contract bindings
	if err := services.Contracts.LoadContracts(addresses); err != nil {
		return nil, fmt.Errorf("failed to initialize contracts: %w", err)
	}

	// Set account private key
	if err := services.Contracts.SetAccountPrivateKey(cfg.Web3.PrivKey); err != nil {
		return nil, fmt.Errorf("failed to set account private key: %w", err)
	}

	log.Infow("contracts initialized",
		"chainId", services.Contracts.ChainID,
		"account", services.Contracts.AccountAddress().Hex())

	// Start process monitor
	log.Info("starting process monitor")
	services.ProcessMon = service.NewProcessMonitor(services.Contracts, services.Storage, monitorInterval)
	if err := services.ProcessMon.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start process monitor: %w", err)
	}

	// Start API service
	log.Infow("starting API service", "host", cfg.API.Host, "port", cfg.API.Port)
	services.API = service.NewAPI(services.Storage, cfg.API.Host, cfg.API.Port, cfg.Web3.Network, config.DefaultConfig[cfg.Web3.Network], cfg.Log.DisableAPI)

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
	services.Sequencer = service.NewSequencer(services.Storage, services.Contracts, cfg.Batch.Time, services.API.API)
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
	if services.ProcessMon != nil {
		services.ProcessMon.Stop()
	}
	services.Storage.Close() // Close storage last
}
