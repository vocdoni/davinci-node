package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/vocdoni/vocdoni-z-sandbox/config"
	"github.com/vocdoni/vocdoni-z-sandbox/internal"
)

const (
	defaultNetwork   = "sep"
	defaultAPIHost   = "0.0.0.0"
	defaultAPIPort   = 9090
	defaultBatchTime = 300 * time.Second
	defaultLogLevel  = "info"
	defaultLogOutput = "stdout"
	defaultDatadir   = ".davinci" // Will be prefixed with user's home directory
	artifactsTimeout = 20 * time.Minute
	monitorInterval  = 10 * time.Second
)

// Version is the build version, set at build time with -ldflags
var Version = internal.Version

// Config holds the application configuration
type Config struct {
	Web3    Web3Config
	API     APIConfig
	Batch   BatchConfig
	Log     LogConfig
	Datadir string
}

// Web3Config holds Ethereum-related configuration
type Web3Config struct {
	PrivKey           string   `mapstructure:"privkey"`
	Network           string   `mapstructure:"network"`
	Rpc               []string `mapstructure:"rpc"`
	ProcessAddr       string   `mapstructure:"process"`
	OrganizationsAddr string   `mapstructure:"orgs"`
}

// APIConfig holds the API-specific configuration
type APIConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// BatchConfig holds batch processing configuration
type BatchConfig struct {
	Time time.Duration `mapstructure:"time"`
}

// LogConfig holds logging configuration
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Output string `mapstructure:"output"`
}

// loadConfig loads configuration from flags, environment variables, and defaults
func loadConfig() (*Config, error) {
	v := viper.New()

	// Set up default values
	// Get user's home directory for default datadir
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		userHomeDir = "."
	}
	defaultDatadirPath := filepath.Join(userHomeDir, defaultDatadir)

	v.SetDefault("web3.network", defaultNetwork)
	v.SetDefault("web3.rpc", []string{})
	v.SetDefault("api.host", defaultAPIHost)
	v.SetDefault("api.port", defaultAPIPort)
	v.SetDefault("batch.time", defaultBatchTime)
	v.SetDefault("log.level", defaultLogLevel)
	v.SetDefault("log.output", defaultLogOutput)
	v.SetDefault("datadir", defaultDatadirPath)

	// Configure flags
	flag.StringP("web3.privkey", "k", "", "private key to use for the Ethereum account (required)")
	flag.StringP("web3.network", "n", defaultNetwork, fmt.Sprintf("network to use %v", config.AvailableNetworks))
	flag.StringSliceP("web3.rpc", "w", []string{}, "web3 rpc endpoint(s), comma-separated")
	flag.StringP("api.host", "a", defaultAPIHost, "API host")
	flag.IntP("api.port", "p", defaultAPIPort, "API port")
	flag.DurationP("batch.time", "b", defaultBatchTime, "sequencer batch max time window (i.e 10m or 1h)")
	flag.String("web3.process", "", "custom process registry contract address (overrides network default)")
	flag.String("web3.orgs", "", "custom organization registry contract address (overrides network default)")
	flag.String("web3.results", "", "custom results registry contract address (overrides network default)")
	flag.StringP("log.level", "l", defaultLogLevel, "log level (debug, info, warn, error, fatal)")
	flag.StringP("log.output", "o", defaultLogOutput, "log output (stdout, stderr or filepath)")
	flag.StringP("datadir", "d", defaultDatadirPath, "data directory for database and storage files")

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
