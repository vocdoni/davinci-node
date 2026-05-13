package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/vocdoni/davinci-node/internal"
)

const (
	defaultRPC                        = "https://ethereum-sepolia-rpc.publicnode.com"
	defaultConsensusAPI               = "https://ethereum-sepolia-beacon-api.publicnode.com"
	defaultAPIHost                    = "0.0.0.0"
	defaultAPIPort                    = 9090
	defaultBatchTime                  = 300 * time.Second
	defaultLogLevel                   = "info"
	defaultLogOutput                  = "stdout"
	defaultLogDisableAPI              = false
	defaultDatadir                    = ".davinci" // Will be prefixed with user's home directory
	defaultGasMultiplier              = 1.2
	artifactsTimeout                  = 20 * time.Minute
	monitorInterval                   = 10 * time.Second
	defaultWorkersBanTimeout          = 30 * time.Minute
	defaultWorkersAuthtokenExpiration = 90 * 24 * time.Hour // 90 days
	defaultWorkerBanFailures          = 3
)

// Version is the build version, set at build time with -ldflags
var Version = internal.Version

// Config holds the application configuration
type Config struct {
	Web3         Web3Config
	API          APIConfig
	Batch        BatchConfig
	Log          LogConfig
	Worker       WorkerConfig
	Metadata     MetadataConfig
	Datadir      string
	ForceCleanup bool `mapstructure:"forceCleanup"` // Force cleanup of all pending items at startup
}

// Web3Config holds Ethereum-related configuration
type Web3Config struct {
	PrivKey       string   `mapstructure:"privkey"`       // Private key for the Ethereum account
	ChainIDs      []uint   `mapstructure:"chainIDs"`      // Chain IDs to use, if defined, limits RPCs and BeaconAPIs, if empty, use all
	RPCs          []string `mapstructure:"rpc"`           // Web3 RPC endpoints, can be multiple
	BeaconAPIs    []string `mapstructure:"capi"`          // Web3 Consensus Beacon API endpoints, can be multiple
	GasMultiplier float64  `mapstructure:"gasMultiplier"` // Gas price multiplier for transactions (default: 1.0)
}

// APIConfig holds the API-specific configuration
type APIConfig struct {
	Host                       string        `mapstructure:"host"`                       // API host address
	Port                       int           `mapstructure:"port"`                       // API port number
	SequencerWorkersSeed       string        `mapstructure:"workersSeed"`                // URL seed for worker authentication
	WorkersAuthtokenExpiration time.Duration `mapstructure:"workersAuthtokenExpiration"` // Expiration time for worker authentication tokens
	WorkersBanTimeout          time.Duration `mapstructure:"workersBanTimeout"`          // Timeout for worker ban
	WorkersFailuresToGetBanned int           `mapstructure:"workersFailuresToGetBanned"` // Number of failed jobs to get banned
}

// BatchConfig holds batch processing configuration
type BatchConfig struct {
	Time time.Duration `mapstructure:"time"` // Maximum time window to wait for batch processing
}

// LogConfig holds logging configuration
type LogConfig struct {
	Level      string `mapstructure:"level"`
	Output     string `mapstructure:"output"`
	DisableAPI bool   `mapstructure:"disableAPI"` // Disable API logging middleware
}

// WorkerConfig holds worker-related configuration
type WorkerConfig struct {
	Timeout      time.Duration `mapstructure:"timeout"`      // Timeout for worker jobs
	Address      string        `mapstructure:"address"`      // Ethereum address for the worker (auto-generated if empty)
	Name         string        `mapstructure:"name"`         // Name of the worker for identification
	Authtoken    string        `mapstructure:"authtoken"`    // Worker authentication token
	SequencerURL string        `mapstructure:"sequencerURL"` // URL seed for master worker endpoint
}

// MetadataConfig holds metadata configuration
type MetadataConfig struct {
	PinataHostnameURL  string `mapstructure:"pinataHostnameURL"`  // Pinata hostname URL
	PinataHostnameJWT  string `mapstructure:"pinataHostnameJWT"`  // Pinata hostname JWT
	PinataGatewayURL   string `mapstructure:"pinataGatewayURL"`   // Pinata gateway URL
	PinataGatewayToken string `mapstructure:"pinataGatewayToken"` // Pinata gateway token
}

// loadConfig loads configuration from flags, environment variables, and defaults
func loadConfig() (*Config, error) {
	cfg := &Config{}

	// Get user's home directory for default datadir
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		userHomeDir = "."
	}
	defaultDatadirPath := filepath.Join(userHomeDir, defaultDatadir)

	// Configure flags

	// web3 config
	flag.StringP("web3.privkey", "k", "", "private key to use for the Ethereum account, should have funds for each available network (required)")
	flag.UintSlice("web3.chainIDs", nil, "chainIDs to limit RPCs and BeaconAPIs, comma-separated, empty for all")
	flag.StringSliceP("web3.rpc", "r", []string{defaultRPC}, "web3 rpc endpoint(s), comma-separated")
	flag.StringSliceP("web3.capi", "c", []string{defaultConsensusAPI}, "consensus api endpoints(s), comma-separated")
	flag.Float64("web3.gasMultiplier", defaultGasMultiplier, "gas price multiplier for transactions (1.0 = default, 2.0 = double gas prices)")
	flag.String("web3.processRegistryAddress", "", "Address of the process registry contract to be used by the sequencer (overrides network default)")
	// sequencer API
	flag.StringP("api.host", "h", defaultAPIHost, "API host")
	flag.IntP("api.port", "p", defaultAPIPort, "API port")
	flag.DurationP("batch.time", "b", defaultBatchTime, "sequencer batch max time window (i.e 10m or 1h)")
	flag.StringP("log.level", "l", defaultLogLevel, "log level (debug, info, warn, error, fatal)")
	flag.StringP("log.output", "o", defaultLogOutput, "log output (stdout, stderr or filepath)")
	flag.Bool("log.disableAPI", defaultLogDisableAPI, "disable API logging middleware")
	flag.StringP("datadir", "d", defaultDatadirPath, "data directory for database and storage files")
	flag.Bool("forceCleanup", false, "force cleanup of all pending verified votes, aggregated batches and state transitions at startup")
	// sequencer workers api flags
	flag.String("api.workersSeed", "", "enable master worker endpoint with URL seed for authentication")
	flag.Duration("api.workersBanTimeout", defaultWorkersBanTimeout, "timeout for worker ban in seconds")
	flag.Duration("api.workersAuthtokenExpiration", defaultWorkersAuthtokenExpiration, "timeout for worker authentication token expiration")
	flag.Int("api.workersFailuresToGetBanned", defaultWorkerBanFailures, "number of failed jobs to get banned")
	// worker mode flags
	flag.Duration("worker.timeout", 1*time.Minute, "worker job timeout duration")
	flag.StringP("worker.address", "a", "", "worker Ethereum address")
	flag.String("worker.name", "", "worker name for identification")
	flag.StringP("worker.authtoken", "t", "", "worker authentication token (required for running in worker mode)")
	flag.StringP("worker.sequencerURL", "w", "", "sequencer URL (required for running in worker mode)")
	// metadata config
	flag.String("metadata.pinataHostnameURL", "https://uploads.pinata.cloud/v3/files", "pinata hostname URL")
	flag.String("metadata.pinataHostnameJWT", "", "pinata hostname JWT")
	flag.String("metadata.pinataGatewayURL", "https://gateway.pinata.cloud/ipfs", "pinata gateway URL")
	flag.String("metadata.pinataGatewayToken", "", "pinata gateway token")

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
		fmt.Fprintf(os.Stderr, "  # Start in multinetwork mode with structured runtime configs\n")
		fmt.Fprintf(os.Stderr, "  davinci-sequencer --web3.privkey=0x123... --web3.rpc=https://network1.rpc.com,https://network2.rpc.com --web3.capi=https://network1.beaconapi.com,https://network2.beaconapi.com\n\n\n")
	}

	// Parse flags
	flag.CommandLine.SortFlags = false
	flag.Parse()

	// Configure Viper
	viper.SetEnvPrefix("DAVINCI")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.BindPFlags(flag.CommandLine); err != nil {
		return nil, fmt.Errorf("error binding flags: %w", err)
	}

	// Unmarshal configuration into struct
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	return cfg, nil
}

// validateConfig validates the loaded configuration
func validateConfig(cfg *Config) error {
	// Validate required fields
	if cfg.Web3.PrivKey == "" {
		return fmt.Errorf("private key is required (use --privkey flag or DAVINCI_WEB3_PRIVKEY environment variable)")
	}

	// Validate gas multiplier
	if cfg.Web3.GasMultiplier <= 0 {
		return fmt.Errorf("gas multiplier must be greater than 0, got: %f", cfg.Web3.GasMultiplier)
	}
	if cfg.Web3.GasMultiplier > 100 {
		return fmt.Errorf("gas multiplier too high (max 100), got: %f", cfg.Web3.GasMultiplier)
	}

	return nil
}
