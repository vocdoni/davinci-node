package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/vocdoni-z-sandbox/config"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/service"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/web3"
	"github.com/vocdoni/vocdoni-z-sandbox/web3/rpc/chainlist"
)

const (
	defaultNetwork   = "sep"
	defaultAPIHost   = "0.0.0.0"
	defaultAPIPort   = 8080
	defaultBatchTime = 60
	defaultLogLevel  = "info"
	defaultLogOutput = "stdout"
	artifactsTimeout = 5 * time.Minute
	monitorInterval  = 2 * time.Second
)

// Version is the build version, set at build time with -ldflags
var Version = "dev"

// Config holds the application configuration
type Config struct {
	Web3  Web3Config
	API   APIConfig
	Batch BatchConfig
	Log   LogConfig
}

// Web3Config holds Ethereum-related configuration
type Web3Config struct {
	PrivKey           string   `mapstructure:"privkey"`
	Network           string   `mapstructure:"network"`
	Rpc               []string `mapstructure:"rpc"`
	ProcessAddr       string   `mapstructure:"process"`
	OrganizationsAddr string   `mapstructure:"orgs"`
	ResultsAddr       string   `mapstructure:"results"`
}

// APIConfig holds the API-specific configuration
type APIConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// BatchConfig holds batch processing configuration
type BatchConfig struct {
	Time int `mapstructure:"time"`
}

// LogConfig holds logging configuration
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Output string `mapstructure:"output"`
}

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

// loadConfig loads configuration from flags, environment variables, and defaults
func loadConfig() (*Config, error) {
	v := viper.New()

	// Set up default values
	v.SetDefault("web3.network", defaultNetwork)
	v.SetDefault("web3.rpc", []string{})
	v.SetDefault("api.host", defaultAPIHost)
	v.SetDefault("api.port", defaultAPIPort)
	v.SetDefault("batch.time", defaultBatchTime)
	v.SetDefault("log.level", defaultLogLevel)
	v.SetDefault("log.output", defaultLogOutput)

	// Configure flags
	flag.StringP("web3.privkey", "k", "", "private key to use for the Ethereum account (required)")
	flag.StringP("web3.network", "n", defaultNetwork, fmt.Sprintf("network to use %v", config.AvailableNetworks))
	flag.StringSliceP("web3.rpc", "w", []string{}, "web3 rpc endpoint(s), comma-separated")
	flag.StringP("api.host", "a", defaultAPIHost, "API host")
	flag.IntP("api.port", "p", defaultAPIPort, "API port")
	flag.IntP("batch.time", "b", defaultBatchTime, "sequencer batch time window in seconds")
	flag.String("web3.process", "", "custom process registry contract address (overrides network default)")
	flag.String("web3.orgs", "", "custom organization registry contract address (overrides network default)")
	flag.String("web3.results", "", "custom results registry contract address (overrides network default)")
	flag.StringP("log.level", "l", defaultLogLevel, "log level (debug, info, warn, error, fatal)")
	flag.StringP("log.output", "o", defaultLogOutput, "log output (stdout, stderr or filepath)")

	// Configure usage information
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "davinci-sequencer v%s\n\n", Version)
		fmt.Fprintf(os.Stderr, "Usage: davinci-sequencer [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment variables are also available with the same name as flags,\n")
		fmt.Fprintf(os.Stderr, "  except for dashes (-) and dots (.) which are replaced by underscores (_).\n")
		fmt.Fprintf(os.Stderr, "  For example, DAVINCI_WEB3_PRIVKEY or DAVINCI_API_HOST\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Start with sepolia network and default settings\n")
		fmt.Fprintf(os.Stderr, "  davinci-sequencer --web3.privkey=0x123...\n\n")
		fmt.Fprintf(os.Stderr, "  # Start with custom RPC endpoints\n")
		fmt.Fprintf(os.Stderr, "  davinci-sequencer --web3.privkey=0x123... --web3.rpc=https://rpc1.com,https://rpc2.com\n\n")
		fmt.Fprintf(os.Stderr, "  # Start with custom contract addresses\n")
		fmt.Fprintf(os.Stderr, "  davinci-sequencer --web3.privkey=0x123... --web3.process_registry=0x456... --web3.org_registry=0x789...\n")
	}

	// Parse flags
	flag.CommandLine.SortFlags = false
	flag.Parse()

	// Configure Viper to use environment variables
	v.SetEnvPrefix("DAVINCI")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Bind flags to Viper
	if err := v.BindPFlags(flag.CommandLine); err != nil {
		return nil, fmt.Errorf("error binding flags: %w", err)
	}

	// Create config struct
	cfg := &Config{}

	// Unmarshal configuration into struct
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	return cfg, nil
}

// validateConfig validates the loaded configuration
func validateConfig(cfg *Config) error {
	// Validate required fields
	if cfg.Web3.PrivKey == "" {
		return fmt.Errorf("private key is required (use --privkey flag or DAVINCI_PRIVKEY environment variable)")
	}

	// Validate network
	validNetwork := false
	for _, n := range config.AvailableNetworks {
		if cfg.Web3.Network == n {
			validNetwork = true
			break
		}
	}
	if !validNetwork {
		return fmt.Errorf("invalid network %s, available networks: %v", cfg.Web3.Network, config.AvailableNetworks)
	}

	return nil
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
	log.Info("downloading circuit artifacts")
	if err := service.DownloadArtifacts(artifactsTimeout); err != nil {
		log.Warnw("failed to download some artifacts", "error", err)
	}

	// Initialize storage
	log.Info("initializing storage")
	services.Storage = storage.New(memdb.New())

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
		endpoints, err := chainlist.EndpointList(0, cfg.Web3.Network, 10)
		if err != nil {
			return nil, fmt.Errorf("failed to get endpoints for network %s: %w", cfg.Web3.Network, err)
		}
		log.Infow("using endpoints from chain list", "chainID", id, "network", cfg.Web3.Network, "endpoints", endpoints)
		w3rpc = endpoints
	}

	// Initialize web3 contracts
	var err error
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
	services.API = service.NewAPI(services.Storage, cfg.API.Host, cfg.API.Port)
	if err := services.API.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start API service: %w", err)
	}

	// Start sequencer service
	log.Infow("starting sequencer service", "batchTimeWindow", time.Duration(cfg.Batch.Time)*time.Second)
	services.Sequencer = service.NewSequencer(services.Storage, time.Duration(cfg.Batch.Time)*time.Second)
	if err := services.Sequencer.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start sequencer service: %w", err)
	}

	log.Info("davinci-sequencer is running")
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
}
