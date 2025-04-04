package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	flag "github.com/spf13/pflag"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/vocdoni-z-sandbox/config"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/service"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/web3"
)

const (
	defaultNetwork   = "sepolia"
	defaultAPIHost   = "0.0.0.0"
	defaultAPIPort   = 8080
	defaultBatchTime = 60
	defaultLogLevel  = "info"
	defaultLogOutput = "stdout"
	artifactsTimeout = 5 * time.Minute
	monitorInterval  = 2 * time.Second
)

var (
	// Version is the build version, set at build time with -ldflags.
	Version = "dev"
)

func main() {
	// Parse command line flags
	privKey := flag.String("privkey", "", "private key to use for the Ethereum account (required)")
	network := flag.String("network", defaultNetwork, fmt.Sprintf("network to use %v", config.AvailableNetworks))
	w3rpc := flag.StringSlice("w3rpc", []string{}, "web3 rpc endpoint(s), comma-separated")
	apiHost := flag.String("apiHost", defaultAPIHost, "API host")
	apiPort := flag.Int("apiPort", defaultAPIPort, "API port")
	batchTime := flag.Int("batchTime", defaultBatchTime, "sequencer batch time window in seconds")
	processRegistry := flag.String("processRegistry", "", "custom process registry contract address (overrides network default)")
	orgRegistry := flag.String("orgRegistry", "", "custom organization registry contract address (overrides network default)")
	resultsRegistry := flag.String("resultsRegistry", "", "custom results registry contract address (overrides network default)")
	logLevel := flag.String("logLevel", defaultLogLevel, "log level (debug, info, warn, error, fatal)")
	logOutput := flag.String("logOutput", defaultLogOutput, "log output (stdout, stderr or filepath)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "davinci-sequencer v%s\n\n", Version)
		fmt.Fprintf(os.Stderr, "Usage: davinci-sequencer [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Start with sepolia network and default settings\n")
		fmt.Fprintf(os.Stderr, "  davinci-sequencer --privkey=0x123...\n\n")
		fmt.Fprintf(os.Stderr, "  # Start with custom RPC endpoints\n")
		fmt.Fprintf(os.Stderr, "  davinci-sequencer --privkey=0x123... --w3rpc=https://rpc1.com,https://rpc2.com\n\n")
		fmt.Fprintf(os.Stderr, "  # Start with custom contract addresses\n")
		fmt.Fprintf(os.Stderr, "  davinci-sequencer --privkey=0x123... --processRegistry=0x456... --orgRegistry=0x789...\n")
	}

	flag.Parse()

	// Initialize logging
	log.Init(*logLevel, *logOutput, nil)

	log.Infow("starting davinci-sequencer", "version", Version)

	// Validate required flags
	if *privKey == "" {
		log.Fatal("private key is required")
	}

	// Validate network
	validNetwork := false
	for _, n := range config.AvailableNetworks {
		if *network == n {
			validNetwork = true
			break
		}
	}
	if !validNetwork {
		log.Fatalf("invalid network %s, available networks: %v", *network, config.AvailableNetworks)
	}

	// Select the contract addresses based on network
	networkConfig, ok := config.DefaultConfig[*network]
	if !ok {
		log.Fatalf("no configuration found for network %s", *network)
	}

	// Override contract addresses if specified
	processRegistryAddr := networkConfig.ProcessRegistrySmartContract
	if *processRegistry != "" {
		processRegistryAddr = *processRegistry
	}

	orgRegistryAddr := networkConfig.OrganizationRegistrySmartContract
	if *orgRegistry != "" {
		orgRegistryAddr = *orgRegistry
	}

	resultsAddr := networkConfig.ResultsSmartContract
	if *resultsRegistry != "" {
		resultsAddr = *resultsRegistry
	}

	log.Infow("using contract addresses",
		"network", *network,
		"processRegistry", processRegistryAddr,
		"orgRegistry", orgRegistryAddr,
		"resultsRegistry", resultsAddr)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Download circuit artifacts
	log.Info("downloading circuit artifacts")
	if err := service.DownloadArtifacts(artifactsTimeout); err != nil {
		log.Warnw("failed to download some artifacts", "error", err)
	}

	// Create storage
	log.Info("initializing storage")
	stg := storage.New(memdb.New())

	// Initialize contracts
	log.Info("initializing web3 contracts")
	var contracts *web3.Contracts
	var err error

	// Default RPC endpoints by network if not provided by user
	if len(*w3rpc) == 0 {
		// Default RPC endpoints for supported networks
		defaultRPCs := map[string][]string{
			"sepolia": {
				"https://rpc.ankr.com/eth_sepolia",
				"https://sepolia.gateway.tenderly.co",
				"https://eth-sepolia.public.blastapi.io",
			},
		}

		if endpoints, ok := defaultRPCs[*network]; ok && len(endpoints) > 0 {
			log.Infow("using default RPC endpoints for network", "network", *network)
			*w3rpc = endpoints
		} else {
			log.Fatal("no RPC endpoints provided and no defaults available for the selected network")
		}
	}

	// Initialize with the first endpoint
	addresses := &web3.Addresses{
		ProcessRegistry:      common.HexToAddress(processRegistryAddr),
		OrganizationRegistry: common.HexToAddress(orgRegistryAddr),
		ResultsRegistry:      common.HexToAddress(resultsAddr),
	}

	contracts, err = web3.LoadContracts(addresses, (*w3rpc)[0])
	if err != nil {
		log.Fatalf("failed to initialize contracts: %v", err)
	}

	// Add additional RPC endpoints
	for i := 1; i < len(*w3rpc); i++ {
		if err := contracts.AddWeb3Endpoint((*w3rpc)[i]); err != nil {
			log.Warnw("failed to add RPC endpoint", "endpoint", (*w3rpc)[i], "error", err)
		}
	}

	// Set the account private key
	if err := contracts.SetAccountPrivateKey(*privKey); err != nil {
		log.Fatalf("failed to set account private key: %v", err)
	}

	log.Infow("contracts initialized", "chainId", contracts.ChainID, "account", contracts.AccountAddress().Hex())

	// Start the process monitor
	log.Info("starting process monitor")
	pm := service.NewProcessMonitor(contracts, stg, monitorInterval)
	if err := pm.Start(ctx); err != nil {
		log.Fatalf("failed to start process monitor: %v", err)
	}
	defer pm.Stop()

	// Start the API service
	log.Infow("starting API service", "host", *apiHost, "port", *apiPort)
	api := service.NewAPI(stg, *apiHost, *apiPort)
	if err := api.Start(ctx); err != nil {
		log.Fatalf("failed to start API service: %v", err)
	}
	defer api.Stop()

	// Start the sequencer service
	log.Infow("starting sequencer service", "batchTimeWindow", time.Duration(*batchTime)*time.Second)
	seq := service.NewSequencer(stg, time.Duration(*batchTime)*time.Second)
	if err := seq.Start(ctx); err != nil {
		log.Fatalf("failed to start sequencer service: %v", err)
	}
	defer seq.Stop()

	log.Info("davinci-sequencer is running")

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	sig := <-sigCh
	log.Infow("received signal, shutting down", "signal", sig.String())
}
