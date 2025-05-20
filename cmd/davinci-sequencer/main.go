package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/vocdoni-z-sandbox/config"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/service"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/web3"
	"github.com/vocdoni/vocdoni-z-sandbox/web3/rpc/chainlist"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/metadb"
)

// Services holds all the running services
type Services struct {
	Contracts  *web3.Contracts
	Storage    *storage.Storage
	ProcessMon *service.ProcessMonitor
	API        *service.APIService
	Sequencer  *service.SequencerService
	Finalizer  *service.FinalizerService
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

	resultsAddr := networkConfig.ResultsSmartContract
	if cfg.Web3.ResultsAddr != "" {
		resultsAddr = cfg.Web3.ResultsAddr
	}

	// Log the contract addresses being used
	log.Infow("using contract addresses",
		"network", cfg.Web3.Network,
		"processRegistry", processRegistryAddr,
		"orgRegistry", orgRegistryAddr,
		"resultsRegistry", resultsAddr)

	// Create the addresses struct
	return &web3.Addresses{
		ProcessRegistry:      common.HexToAddress(processRegistryAddr),
		OrganizationRegistry: common.HexToAddress(orgRegistryAddr),
		ResultsRegistry:      common.HexToAddress(resultsAddr),
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
	services.Contracts, err = web3.New(w3rpc)
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
	services.API = service.NewAPI(services.Storage, cfg.API.Host, cfg.API.Port, cfg.Web3.Network)
	if err := services.API.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start API service: %w", err)
	}

	// Start sequencer service
	log.Infow("starting sequencer service", "batchTimeWindow", cfg.Batch.Time.String())
	services.Sequencer = service.NewSequencer(services.Storage, services.Contracts, cfg.Batch.Time)
	if err := services.Sequencer.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start sequencer service: %w", err)
	}

	// Start finalizer service
	log.Infow("starting finalizer service", "monitorInterval", time.Minute)
	services.Finalizer = service.NewFinalizer(services.Storage, services.Storage.StateDB(), time.Minute)
	if err := services.Finalizer.Start(ctx, time.Minute); err != nil {
		return nil, fmt.Errorf("failed to start finalizer service: %w", err)
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
	if services.Finalizer != nil {
		services.Finalizer.Stop()
	}
	if services.Sequencer != nil {
		services.Sequencer.Stop()
	}
	if services.API != nil {
		services.API.Stop()
	}
	if services.ProcessMon != nil {
		services.ProcessMon.Stop()
	}
}
