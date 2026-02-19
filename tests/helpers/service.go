package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/consensys/gnark/logger"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	tc "github.com/testcontainers/testcontainers-go/modules/compose"
	c3config "github.com/vocdoni/census3-bigquery/config"
	c3service "github.com/vocdoni/census3-bigquery/service"
	"github.com/vocdoni/davinci-node/api/client"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/sequencer"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/util"
	"github.com/vocdoni/davinci-node/web3"
	"github.com/vocdoni/davinci-node/web3/txmanager"
	"github.com/vocdoni/davinci-node/workers"
	"golang.org/x/mod/modfile"
)

// TestServices struct holds all test services
type TestServices struct {
	API              *service.APIService
	Census3          *c3service.Service
	Sequencer        *sequencer.Sequencer
	CensusDownloader *service.CensusDownloader
	Storage          *storage.Storage
	Contracts        *web3.Contracts
	HTTPClient       *client.HTTPclient
}

func NewTestServices(
	ctx context.Context,
	tempDir string,
	workerSecret string,
	workerTokenExpiration time.Duration,
	workerTimeout time.Duration,
	banRules *workers.WorkerBanRules,
) (*TestServices, func(), error) {
	// Initialize census3 service
	c3srv, c3cleanup, err := setupCensusService()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to setup census3 service: %w", err)
	}
	// Initialize the web3 contracts
	contracts, web3Cleanup, err := setupWeb3(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to setup web3: %w", err)
	}

	kv, err := metadb.New(db.TypePebble, tempDir)
	if err != nil {
		web3Cleanup() // Clean up web3 if db creation fails
		return nil, nil, fmt.Errorf("failed to create database: %w", err)
	}
	stg := storage.New(kv)

	services := &TestServices{
		Census3:   c3srv,
		Storage:   stg,
		Contracts: contracts,
	}

	// Start sequencer service
	sequencer.AggregatorTickerInterval = time.Second * 2
	sequencer.NewProcessMonitorInterval = time.Second * 5
	vp := service.NewSequencer(stg, contracts, DefaultBatchTimeWindow, nil)
	seqCtx, seqCancel := context.WithCancel(ctx)
	if err := vp.Start(seqCtx); err != nil {
		seqCancel()
		web3Cleanup() // Clean up web3 if sequencer fails to start
		return nil, nil, fmt.Errorf("failed to start sequencer: %w", err)
	}
	services.Sequencer = vp.Sequencer

	if IsDebugTest() {
		logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
		log.Info("Debug prover is disabled in non-testing context")
	}

	// Start census downloader
	cd := service.NewCensusDownloader(contracts, services.Storage, service.DefaultCensusDownloaderConfig)
	if err := cd.Start(ctx); err != nil {
		vp.Stop()
		seqCancel()
		web3Cleanup()
		return nil, nil, fmt.Errorf("failed to start census downloader: %w", err)
	}
	services.CensusDownloader = cd

	// Start StateSync
	stateSync := service.NewStateSync(contracts, stg)
	if err := stateSync.Start(ctx); err != nil {
		cd.Stop()
		vp.Stop()
		seqCancel()
		web3Cleanup() // Clean up web3 if process monitor fails to start
		return nil, nil, fmt.Errorf("failed to start state sync: %v", err)
	}

	// Start process monitor
	pm := service.NewProcessMonitor(contracts, stg, cd, stateSync, time.Second*2)
	if err := pm.Start(ctx); err != nil {
		cd.Stop()
		vp.Stop()
		seqCancel()
		web3Cleanup() // Clean up web3 if process monitor fails to start
		return nil, nil, fmt.Errorf("failed to start process monitor: %w", err)
	}
	// Start API service
	web3Conf := config.DavinciWeb3Config{
		ProcessRegistrySmartContract: contracts.ContractsAddresses.ProcessRegistry.String(),
		ResultsZKVerifier:            contracts.ContractsAddresses.ResultsZKVerifier.String(),
		StateTransitionZKVerifier:    contracts.ContractsAddresses.StateTransitionZKVerifier.String(),
	}
	api, err := setupAPI(ctx, stg, workerSecret, workerTokenExpiration, workerTimeout, banRules, web3Conf)
	if err != nil {
		pm.Stop()
		cd.Stop()
		vp.Stop()
		seqCancel()
		web3Cleanup() // Clean up web3 if API fails to start
		return nil, nil, fmt.Errorf("failed to setup API: %w", err)
	}
	services.API = api
	services.HTTPClient, err = httpClient(DefaultAPIPort)
	if err != nil {
		api.Stop()
		pm.Stop()
		cd.Stop()
		vp.Stop()
		seqCancel()
		web3Cleanup()
		return nil, nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	// Create a combined cleanup function
	cleanup := func() {
		seqCancel()
		api.Stop()
		cd.Stop()
		pm.Stop()
		vp.Stop()
		stg.Close()
		c3cleanup()
		web3Cleanup()
	}

	return services, cleanup, nil
}

// httpClient creates a new API client for testing.
func httpClient(port int) (*client.HTTPclient, error) {
	return client.New(fmt.Sprintf("http://127.0.0.1:%d", port))
}

// setupAPI creates and starts a new API server for testing.
// It returns the server port.
func setupAPI(
	ctx context.Context,
	db *storage.Storage,
	workerSeed string,
	workerTokenExpiration time.Duration,
	workerTimeout time.Duration,
	banRules *workers.WorkerBanRules,
	web3Conf config.DavinciWeb3Config,
) (*service.APIService, error) {
	api := service.NewAPI(db, "127.0.0.1", DefaultAPIPort, "test", web3Conf, false)
	api.SetWorkerConfig(workerSeed, workerTokenExpiration, workerTimeout, banRules)
	if err := api.Start(ctx); err != nil {
		return nil, err
	}

	// Wait for the HTTP server to start
	time.Sleep(500 * time.Millisecond)
	return api, nil
}

// setupCensusService creates and starts a new census3-bigquery service for
// testing.
func setupCensusService() (*c3service.Service, func(), error) {
	// create temp dir for census3-bigquery
	tempDir, err := os.MkdirTemp("", "census3-bigquery-test-")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create temp dir for census3-bigquery: %w", err)
	}

	srv, err := c3service.New(&c3config.Config{
		APIPort:       DefaultCensus3Port,
		DataDir:       tempDir,
		MaxCensusSize: 1000000,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create census3-bigquery service: %w", err)
	}

	go func() {
		if err := srv.Start(); err != nil {
			log.Errorw(err, "census3-bigquery service exited with error")
		}
	}()
	return srv, func() {
		srv.Stop()
		if err := os.RemoveAll(tempDir); err != nil {
			log.Warnw("failed to remove census3-bigquery temp dir", "error", err)
		}
	}, nil
}

// setupWeb3 sets up the web3 contracts for testing. It deploys the contracts
// if the environment variables are not set, if they are set it loads the
// contracts from the environment variables. It returns the contracts object
// and a cleanup function that should be called when done.
func setupWeb3(ctx context.Context) (*web3.Contracts, func(), error) {
	// Get the environment variables
	var (
		privKey                       = os.Getenv(PrivKeyEnvVarName)
		rpcUrl                        = os.Getenv(RPCUrlEnvVarName)
		processRegistryAddr           = os.Getenv(ProcessRegistryEnvVarName)
		stateTransitionZKVerifierAddr = os.Getenv(StateTransitionVerifierEnvVarName)
		resultsZKVerifierAddr         = os.Getenv(ResultsVerifierEnvVarName)
	)
	// Check if the environment variables are set to run the tests over local
	// geth node or remote blockchain environment
	localEnv := privKey == "" || rpcUrl == "" || processRegistryAddr == "" ||
		resultsZKVerifierAddr == "" || stateTransitionZKVerifierAddr == ""

	// Store cleanup functions
	var cleanupFuncs []func()
	cleanup := func() {
		// Execute cleanup functions in reverse order
		for i := len(cleanupFuncs) - 1; i >= 0; i-- {
			cleanupFuncs[i]()
		}
	}

	var deployerUrl string
	if localEnv {
		// Generate a random port for geth HTTP RPC
		anvilPort := util.RandomInt(10000, 20000)
		rpcUrl = fmt.Sprintf("http://localhost:%d", anvilPort)
		// Set environment variables for docker-compose in the process environment
		composeEnv := make(map[string]string)
		composeEnv[AnvilPortEnvVarName] = fmt.Sprintf("%d", anvilPort)
		composeEnv[DeployerServerPortEnvVarName] = fmt.Sprintf("%d", anvilPort+1)
		composeEnv[PrivKeyEnvVarName] = LocalAccountPrivKey

		// get branch and commit from the environment variables
		if branchName := os.Getenv(ContractsBranchNameEnvVarName); branchName != "" {
			composeEnv[ContractsBranchNameEnvVarName] = branchName
		}
		if commitHash := os.Getenv(ContractsCommitHashEnvVarName); commitHash != "" {
			composeEnv[ContractsCommitHashEnvVarName] = commitHash
		} else {
			// get it from the go mod file
			modData, err := os.ReadFile("../go.mod")
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read go.mod file: %w", err)
			}
			modFile, err := modfile.Parse("go.mod", modData, nil)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to parse go.mod file: %w", err)
			}
			// get the commit hash from the replace directive
			for _, r := range modFile.Require {
				if r.Mod.Path != "github.com/vocdoni/davinci-contracts" {
					continue
				}
				if versionParts := strings.Split(r.Mod.Version, "-"); len(versionParts) == 3 {
					composeEnv[ContractsCommitHashEnvVarName] = versionParts[2]
					break
				}
				if versionParts := strings.Split(r.Mod.Version, "."); len(versionParts) == 3 {
					composeEnv[ContractsCommitHashEnvVarName] = r.Mod.Version
					break
				}
				return nil, nil, fmt.Errorf("cannot parse davinci-contracts version: %s", r.Mod.Version)

			}
		}

		log.Infow("deploying contracts in local environment",
			"commit", composeEnv[ContractsCommitHashEnvVarName],
			"branch", composeEnv[ContractsBranchNameEnvVarName])

		// Create docker-compose instance
		compose, err := tc.NewDockerCompose("docker/docker-compose.yml")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create docker compose: %w", err)
		}
		ctx2, cancel := context.WithCancel(ctx)
		// Register cleanup for context cancellation
		cleanupFuncs = append(cleanupFuncs, cancel)

		// Start docker-compose
		log.Infow("starting Anvil docker compose", "gethPort", anvilPort)
		err = compose.WithEnv(composeEnv).Up(ctx2, tc.Wait(true), tc.RemoveOrphans(true))
		if err != nil {
			cleanup() // Clean up what we've done so far
			return nil, nil, fmt.Errorf("failed to start docker compose: %w", err)
		}

		// Register cleanup for docker compose shutdown
		cleanupFuncs = append(cleanupFuncs, func() {
			downCtx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()
			if downErr := compose.Down(downCtx, tc.RemoveOrphans(true), tc.RemoveVolumes(true)); downErr != nil {
				log.Warnw("failed to stop docker compose", "error", downErr)
			}
		})

		deployerCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
		defer cancel()
		// Get the enpoint of the deployer service
		deployerContainer, err := compose.ServiceContainer(deployerCtx, "deployer")
		if err != nil {
			cleanup() // Clean up what we've done so far
			return nil, nil, fmt.Errorf("failed to get deployer container: %w", err)
		}
		deployerUrl, err = deployerContainer.Endpoint(deployerCtx, "http")
		if err != nil {
			cleanup() // Clean up what we've done so far
			return nil, nil, fmt.Errorf("failed to get deployer endpoint: %w", err)
		}
	}

	// Wait for the RPC to be ready
	err := web3.WaitReadyRPC(ctx, rpcUrl)
	if err != nil {
		cleanup() // Clean up what we've done so far
		return nil, nil, fmt.Errorf("failed to wait for RPC: %w", err)
	}

	// Initialize the contracts object
	contracts, err := web3.New([]string{rpcUrl}, "", 1.0)
	if err != nil {
		cleanup() // Clean up what we've done so far
		return nil, nil, fmt.Errorf("failed to create web3 contracts: %w", err)
	}

	// Define contracts addresses or deploy them
	if localEnv {
		type deployerResponse struct {
			Txs []struct {
				ContractName    string `json:"contractName"`
				ContractAddress string `json:"contractAddress"`
			} `json:"transactions"`
		}

		// Wait until contracts are deployed and get their addresses from
		// deployer
		contractsCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		var contractsAddresses *web3.Addresses
		for contractsAddresses == nil {
			select {
			case <-contractsCtx.Done():
				cleanup() // Clean up what we've done so far
				return nil, nil, fmt.Errorf("timeout waiting for contracts to be deployed")
			case <-time.After(5 * time.Second):
				// Check if the contracts are deployed making an http request
				// to /addresses.json
				endpoint := fmt.Sprintf("%s/addresses.json", deployerUrl)
				res, err := http.Get(endpoint)
				if err != nil {
					log.Infow("waiting for contracts to be deployed",
						"err", err,
						"deployUrl", endpoint)
					continue
				}
				if res.StatusCode != http.StatusOK {
					if err := res.Body.Close(); err != nil {
						log.Warnw("failed to close deployer response body", "error", err)
					}
					log.Infow("waiting for contracts to be deployed",
						"status", res.StatusCode,
						"deployUrl", endpoint)
					continue
				}
				// Decode the response
				var deployerResp deployerResponse
				err = json.NewDecoder(res.Body).Decode(&deployerResp)
				if err := res.Body.Close(); err != nil {
					log.Warnw("failed to close deployer response body", "error", err)
				}
				if err != nil {
					cleanup() // Clean up what we've done so far
					return nil, nil, fmt.Errorf("failed to decode deployer response: %w", err)
				}
				contractsAddresses = new(web3.Addresses)
				log.Infow("contracts addresses from deployer",
					"logs", deployerResp.Txs)
				for _, tx := range deployerResp.Txs {
					switch tx.ContractName {
					case "ProcessRegistry":
						contractsAddresses.ProcessRegistry = common.HexToAddress(tx.ContractAddress)
					case "StateTransitionVerifierGroth16":
						contractsAddresses.StateTransitionZKVerifier = common.HexToAddress(tx.ContractAddress)
					case "ResultsVerifierGroth16":
						contractsAddresses.ResultsZKVerifier = common.HexToAddress(tx.ContractAddress)
					}
				}
			}
		}
		// Set the private key for the sequencer
		err = contracts.SetAccountPrivateKey(util.TrimHex(LocalAccountPrivKey))
		if err != nil {
			cleanup() // Clean up what we've done so far
			return nil, nil, fmt.Errorf("failed to set account private key: %w", err)
		}
		// Load the contracts addresses into the contracts object
		err = contracts.LoadContracts(contractsAddresses)
		if err != nil {
			cleanup() // Clean up what we've done so far
			return nil, nil, fmt.Errorf("failed to load contracts: %w", err)
		}
		log.Infow("contracts deployed and loaded",
			"chainId", contracts.ChainID,
			"addresses", contractsAddresses)
	} else {
		// Set the private key for the sequencer
		err = contracts.SetAccountPrivateKey(util.TrimHex(privKey))
		if err != nil {
			cleanup() // Clean up what we've done so far
			return nil, nil, fmt.Errorf("failed to set account private key: %w", err)
		}
		// Create the contracts object with the addresses from the environment
		err = contracts.LoadContracts(&web3.Addresses{
			ProcessRegistry:           common.HexToAddress(processRegistryAddr),
			ResultsZKVerifier:         common.HexToAddress(resultsZKVerifierAddr),
			StateTransitionZKVerifier: common.HexToAddress(stateTransitionZKVerifierAddr),
		})
		if err != nil {
			cleanup() // Clean up what we've done so far
			return nil, nil, fmt.Errorf("failed to load contracts: %w", err)
		}
	}

	// Start the transaction manager
	txm, err := txmanager.New(ctx, contracts.Web3Pool(), contracts.Client(), contracts.Signer(), txmanager.DefaultConfig(contracts.ChainID))
	if err != nil {
		cleanup() // Clean up what we've done so far
		return nil, nil, fmt.Errorf("failed to create transaction manager: %w", err)
	}
	txm.Start(ctx)
	contracts.SetTxManager(txm)
	cleanupFuncs = append(cleanupFuncs, func() {
		txm.Stop()
	})
	// Set contracts ABIs
	contracts.ContractABIs = &web3.ContractABIs{
		ProcessRegistry:           contracts.ProcessRegistryABI(),
		StateTransitionZKVerifier: contracts.StateTransitionVerifierABI(),
		ResultsZKVerifier:         contracts.ResultsVerifierABI(),
	}
	// Return the contracts object and cleanup function
	return contracts, cleanup, nil
}
