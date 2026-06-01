package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/vocdoni/davinci-node/client"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

var (
	action            string
	logLevel          string
	globalTimeout     time.Duration
	processConfigPath string
)

type actionHandler func(context.Context, *client.Client, *client.ProcessConfig) bool

var availableActions = map[string]actionHandler{
	client.CreateProcessAction: createAction,
	client.SubmitVoteAction:    submitAction,
	client.StopProcessAction:   stopAction,
}

func loadConfig() (*client.ClientConfig, error) {
	conf := new(client.ClientConfig)
	// Web3 config
	flag.StringP("web3.privkey", "k", "", "private key to use for the Ethereum account, should have funds for each available network (required)")
	flag.StringSliceP("web3.rpc", "r", nil, "web3 rpc endpoint(s), comma-separated")
	flag.StringSlice("web3.bapi", nil, "consensus api endpoints(s), comma-separated")
	flag.StringSlice("web3.processRegistryContract", nil, "'chainID:0xaddress' of the process registry smart contract, if defined, it will be included in the available networks if a valid RPC endpoint is provided")
	// Sequencer config
	flag.StringP("sequencer", "s", "https://sequencer5.davinci.vote", "Davinci sequencer endpoint")
	flag.String("census3", "https://c3-dev.davinci.vote", "Census3 service endpoint")
	// Global config
	flag.DurationVar(&globalTimeout, "globalTimeout", 20*time.Minute, "global timeout for all requests")
	flag.StringVarP(&action, "action", "a", "all", "action to perform (create|stop|vote)")
	flag.StringVar(&logLevel, "logLevel", log.LogLevelInfo, "debug level (debug, info, warn, error)")
	flag.StringVarP(&processConfigPath, "processConfig", "c", "cmd/cli/process-config.example.json", "path to process config file")
	// Parse flags
	flag.Parse()
	// Customize environment variables with prefix and replacer
	viper.SetEnvPrefix("DAVINCI_CLI")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	// Bind flags
	if err := viper.BindPFlags(flag.CommandLine); err != nil {
		return nil, fmt.Errorf("error binding flags: %w", err)
	}
	// Unmarshal config
	if err := viper.Unmarshal(conf); err != nil {
		return nil, fmt.Errorf("error unmarshalling config: %w", err)
	}
	return conf, nil
}

func main() {
	// Load and check config
	config, err := loadConfig()
	if err != nil {
		log.Errorf("error loading config: %v", err)
		return
	}

	// Init logger
	log.Init(logLevel, "stdout", nil)

	// Load process config
	processConfig := new(client.ProcessConfig)
	if err := processConfig.Load(processConfigPath); err != nil {
		log.Errorf("error loading process config: %v", err)
		return
	}

	// Create a context with the defined timeout
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Initialize CLI services
	cliSrv, err := client.NewClient(ctx, config)
	if err != nil {
		log.Errorf("failed to initialize client: %v", err)
		return
	}

	// Get the handler for the desired action, by default all handler
	handler, ok := availableActions[action]
	if !ok {
		handler = all
		// Use CreateProcessAction by default to validate the process config
		action = client.CreateProcessAction
	}

	// Check if process config is valid for the desired action
	if !processConfig.Valid(action) {
		log.Error("no valid process config provided")
		return
	}

	_ = handler(ctx, cliSrv, processConfig)
}

func createAction(ctx context.Context, cli *client.Client, conf *client.ProcessConfig) bool {
	// Create process
	var err error
	conf.ProcessID, err = cli.CreateProcess(conf)
	if err != nil {
		log.Errorf("failed to create process: %v", err)
		return false
	}
	log.Infow("process created", "processID", conf.ProcessID.String())
	return true
}

func submitAction(ctx context.Context, cli *client.Client, conf *client.ProcessConfig) bool {
	// Create votes
	votes, _, err := cli.CreateRandomVotes(conf)
	if err != nil {
		log.Errorf("failed to create vote: %v", err)
		return false
	}
	log.Infow("votes created, submitting...", "votes", len(votes))

	// Submit votes
	voteIDs, err := cli.SubmitVotes(votes...)
	if err != nil {
		log.Errorf("failed to submit vote: %v", err)
		return false
	}
	log.Infow("votes submitted", "voteIDs", voteIDs)

	// Confirm votes
	if err := client.WaitUntilCondition(ctx, time.Second, func() (bool, error) {
		ok, _, err := cli.EnsureVotesStatus(conf.ProcessID, voteIDs, client.VoteIDStatusSettled)
		if err != nil {
			return false, err
		}
		return ok, nil
	}); err != nil {
		log.Errorf("failed to wait for votes to be settled: %v", err)
		return false
	}
	log.Infow("all votes settled", "processID", conf.ProcessID.String())

	// Create overwrites
	votes, _, err = cli.CreateRandomVotes(conf)
	if err != nil {
		log.Errorf("failed to create vote: %v", err)
		return false
	}
	log.Infow("vote overwrites created, submitting...", "overwrites", len(votes))

	// Submit overwrites
	overwriteIDs, err := cli.SubmitVotes(votes...)
	if err != nil {
		log.Errorf("failed to submit overwrite: %v", err)
		return false
	}
	log.Infow("overwrites submitted", "overwriteIDs", overwriteIDs)

	// Confirm overwrites
	if err := client.WaitUntilCondition(ctx, time.Second, func() (bool, error) {
		ok, _, err := cli.EnsureVotesStatus(conf.ProcessID, overwriteIDs, client.VoteIDStatusSettled)
		if err != nil {
			return false, err
		}
		return ok, nil
	}); err != nil {
		log.Errorf("failed to wait for overwrites to be settled: %v", err)
		return false
	}
	log.Infow("all overwrites settled", "processID", conf.ProcessID.String())
	return true
}

func stopAction(ctx context.Context, cli *client.Client, conf *client.ProcessConfig) bool {
	// Stop process
	if err := cli.StopProcess(conf.ProcessID); err != nil {
		log.Errorf("failed to stop process: %v", err)
		return false
	}
	log.Infow("process stopped", "processID", conf.ProcessID.String())

	// Wait for results
	if err := client.WaitUntilCondition(ctx, time.Second, func() (bool, error) {
		process, err := cli.OnChainProcess(conf.ProcessID)
		if err != nil {
			return false, err
		}
		return process.Status == types.ProcessStatusResults, nil
	}); err != nil {
		log.Errorf("failed to wait for process to stop: %v", err)
		return false
	}

	// Fetch results
	results, err := cli.OnchainProcessResults(conf.ProcessID)
	if err != nil {
		log.Errorf("failed to fetch results: %v", err)
		return false
	}
	log.Infow("results fetched", "results", results)
	return true
}

func all(ctx context.Context, cli *client.Client, conf *client.ProcessConfig) bool {
	if !createAction(ctx, cli, conf) {
		return false
	}
	if !submitAction(ctx, cli, conf) {
		return false
	}
	return stopAction(ctx, cli, conf)
}
