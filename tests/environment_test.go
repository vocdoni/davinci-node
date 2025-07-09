package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	tc "github.com/testcontainers/testcontainers-go/modules/compose"
	"github.com/vocdoni/davinci-node/api/client"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/sequencer"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/util"
	"github.com/vocdoni/davinci-node/web3"
	"github.com/vocdoni/davinci-node/workers"
	"golang.org/x/mod/modfile"
)

const (
	// envarionment variable names
	deployerServerPortEnvVarName      = "DEPLOYER_SERVER"                        // environment variable name for deployer server port
	contractsBranchNameEnvVarName     = "SEQUENCER_CONTRACTS_BRANCH"             // environment variable name for z-contracts branch
	contractsCommitHashEnvVarName     = "SEQUENCER_CONTRACTS_COMMIT"             // environment variable name for z-contracts commit hash
	privKeyEnvVarName                 = "SEQUENCER_PRIV_KEY"                     // environment variable name for private key
	rpcUrlEnvVarName                  = "SEQUENCER_RPC_URL"                      // environment variable name for RPC URL
	anvilPortEnvVarName               = "ANVIL_PORT_RPC_HTTP"                    // environment variable name for Anvil port
	orgRegistryEnvVarName             = "SEQUENCER_ORGANIZATION_REGISTRY"        // environment variable name for organization registry
	processRegistryEnvVarName         = "SEQUENCER_PROCESS_REGISTRY"             // environment variable name for process registry
	resultsVerifierEnvVarName         = "SEQUENCER_RESULTS_ZK_VERIFIER"          // environment variable name for results zk verifier
	stateTransitionVerifierEnvVarName = "SEQUENCER_STATE_TRANSITION_ZK_VERIFIER" // environment variable name for state transition zk verifier
)

// testServices struct holds all test services
type testServices struct {
	api            *service.APIService
	sequencer      *sequencer.Sequencer
	storage        *storage.Storage
	contracts      *web3.Contracts
	processMonitor *service.ProcessMonitor
	anvil          *anvilTestService // Anvil service for local testing
}

func (s *testServices) Stop() {
	if err := s.anvil.Stop(); err != nil {
		log.Errorw(err, "failed to stop Anvil service")
	}
	s.api.Stop()
	if err := s.sequencer.Stop(); err != nil {
		log.Errorw(err, "failed to stop sequencer service")
	}
	s.processMonitor.Stop()
	s.storage.Close()
}

type anvilTestService struct {
	ctx      context.Context    // context for the service
	cancel   context.CancelFunc // cancel function for the context
	instance tc.ComposeStack
}

func (s *anvilTestService) Stop() error {
	if s.instance == nil {
		return nil // nothing to stop
	}

	defer s.cancel()
	downCtx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	return s.instance.Down(downCtx, tc.RemoveOrphans(true), tc.RemoveVolumes(true))
}

func newTestServices(ctx context.Context, workerSecret string, workerTimeout time.Duration, banRules *workers.WorkerBanRules) (*testServices, error) {
	// Initialize the web3 contracts
	contracts, anvilSrv, err := setupWeb3(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to setup web3 contracts: %w", err)
	}

	// Create a temporary directory for the metadb
	if err := os.MkdirAll(os.TempDir(), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create temporary directory: %w", err)
	}
	kv, err := metadb.New(db.TypePebble, os.TempDir())
	if err != nil {
		return nil, fmt.Errorf("failed to create metadb: %w", err)
	}
	stg := storage.New(kv)

	services := &testServices{
		storage:   stg,
		contracts: contracts,
		anvil:     anvilSrv,
	}

	// Start sequencer service
	sequencer.AggregatorTickerInterval = time.Second * 2
	sequencer.NewProcessMonitorInterval = time.Second * 5
	vp := service.NewSequencer(stg, contracts, time.Second*30, nil)
	if err := vp.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start sequencer: %w", err)
	}
	services.sequencer = vp.Sequencer

	// Start process monitor
	pm := service.NewProcessMonitor(contracts, stg, time.Second*2)
	if err := pm.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start process monitor: %w", err)
	}
	services.processMonitor = pm

	// Start API service
	api, err := setupAPI(ctx, stg, workerSecret, workerTimeout, banRules)
	if err != nil {
		return nil, fmt.Errorf("failed to setup the API: %w", err)
	}
	services.api = api

	return services, nil
}

// newTestAPIClient creates a new API client for testing.
func newTestAPIClient(port int) (*client.HTTPclient, error) {
	return client.New(fmt.Sprintf("http://127.0.0.1:%d", port))
}

// setupAPI creates and starts a new API server for testing.
// It returns the server port.
func setupAPI(
	ctx context.Context,
	db *storage.Storage,
	workerSeed string,
	workerTimeout time.Duration,
	banRules *workers.WorkerBanRules,
) (*service.APIService, error) {
	tmpPort := util.RandomInt(40000, 60000)

	api := service.NewAPI(db, "127.0.0.1", tmpPort, "test", false)
	api.SetWorkerConfig(workerSeed, workerTimeout, banRules)
	if err := api.Start(ctx); err != nil {
		return nil, err
	}

	// Wait for the HTTP server to start
	time.Sleep(500 * time.Millisecond)
	return api, nil
}

// setupWeb3 sets up the web3 contracts for testing. It deploys the contracts
// if the environment variables are not set, if they are set it loads the
// contracts from the environment variables. It returns the contracts object.
func setupWeb3(ctx context.Context) (*web3.Contracts, *anvilTestService, error) {
	// Get the environment variables
	var (
		privKey                       = os.Getenv(privKeyEnvVarName)
		rpcUrl                        = os.Getenv(rpcUrlEnvVarName)
		orgRegistryAddr               = os.Getenv(orgRegistryEnvVarName)
		processRegistryAddr           = os.Getenv(processRegistryEnvVarName)
		stateTransitionZKVerifierAddr = os.Getenv(stateTransitionVerifierEnvVarName)
		resultsZKVerifierAddr         = os.Getenv(resultsVerifierEnvVarName)
	)
	// Check if the environment variables are set to run the tests over local
	// geth node or remote blockchain environment
	localEnv := privKey == "" || rpcUrl == "" || orgRegistryAddr == "" ||
		processRegistryAddr == "" || resultsZKVerifierAddr == "" || stateTransitionZKVerifierAddr == ""
	var deployerUrl string
	srv := &anvilTestService{}
	if localEnv {
		// Generate a random port for geth HTTP RPC
		anvilPort := util.RandomInt(10000, 20000)
		rpcUrl = fmt.Sprintf("http://localhost:%d", anvilPort)
		// Set environment variables for docker-compose in the process environment
		composeEnv := make(map[string]string)
		composeEnv[anvilPortEnvVarName] = fmt.Sprintf("%d", anvilPort)
		composeEnv[deployerServerPortEnvVarName] = fmt.Sprintf("%d", anvilPort+1)
		composeEnv[privKeyEnvVarName] = testLocalAccountPrivKey

		// get branch and commit from the environment variables
		if branchName := os.Getenv(contractsBranchNameEnvVarName); branchName != "" {
			composeEnv[contractsBranchNameEnvVarName] = branchName
		}
		if commitHash := os.Getenv(contractsCommitHashEnvVarName); commitHash != "" {
			composeEnv[contractsCommitHashEnvVarName] = commitHash
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
				versionParts := strings.Split(r.Mod.Version, "-")
				if len(versionParts) < 3 {
					return nil, nil, fmt.Errorf("invalid version format in go.mod: %s", r.Mod.Version)
				}
				composeEnv[contractsCommitHashEnvVarName] = versionParts[2]
				break
			}
		}

		log.Info("environment variables for docker-compose", composeEnv)

		// Create docker-compose instance
		compose, err := tc.NewDockerCompose("docker/docker-compose.yml")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create docker-compose instance: %w", err)
		}
		srv.instance = compose
		srv.ctx, srv.cancel = context.WithCancel(ctx)
		// Start docker-compose
		log.Infow("starting Anvil docker compose", "gethPort", anvilPort)
		err = compose.WithEnv(composeEnv).Up(srv.ctx, tc.Wait(true), tc.RemoveOrphans(true))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to start docker-compose: %w", err)
		}
		deployerCtx, cancel := context.WithTimeout(srv.ctx, 1*time.Minute)
		defer cancel()
		// Get the enpoint of the deployer service
		deployerContainer, err := compose.ServiceContainer(deployerCtx, "deployer")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get deployer container: %w", err)
		}
		deployerUrl, err = deployerContainer.Endpoint(deployerCtx, "http")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get deployer endpoint: %w", err)
		}
	}

	// Wait for the RPC to be ready
	if err := web3.WaitReadyRPC(ctx, rpcUrl); err != nil {
		return nil, nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

	// Initialize the contracts object
	contracts, err := web3.New([]string{rpcUrl})
	if err != nil {
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
		contractsCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
		defer cancel()
		var contractsAddresses *web3.Addresses
		for contractsAddresses == nil {
			select {
			case <-contractsCtx.Done():
				return nil, nil, fmt.Errorf("timed out waiting for contracts to be deployed: %w", contractsCtx.Err())
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
					return nil, nil, fmt.Errorf("failed to get contracts addresses from deployer: %s", res.Status)
				}
				defer func() {
					if err := res.Body.Close(); err != nil {
						log.Errorw(err, "failed to close response body")
					}
				}()
				// Decode the response
				var deployerResp deployerResponse
				if err := json.NewDecoder(res.Body).Decode(&deployerResp); err != nil {
					return nil, nil, fmt.Errorf("failed to decode deployer response: %w", err)
				}

				contractsAddresses = new(web3.Addresses)
				log.Infow("contracts addresses from deployer",
					"logs", deployerResp.Txs)
				for _, tx := range deployerResp.Txs {
					switch tx.ContractName {
					case "OrganizationRegistry":
						contractsAddresses.OrganizationRegistry = common.HexToAddress(tx.ContractAddress)
					case "ProcessRegistry":
						contractsAddresses.ProcessRegistry = common.HexToAddress(tx.ContractAddress)
					case "StateTransitionVerifierGroth16":
						contractsAddresses.StateTransitionZKVerifier = common.HexToAddress(tx.ContractAddress)
					case "ResultsVerifierGroth16":
						contractsAddresses.ResultsZKVerifier = common.HexToAddress(tx.ContractAddress)
					default:
						log.Infow("unknown contract name", "name", tx.ContractName)
					}
				}
			}
		}
		// Set the private key for the sequencer
		if err := contracts.SetAccountPrivateKey(util.TrimHex(testLocalAccountPrivKey)); err != nil {
			return nil, nil, fmt.Errorf("failed to set account private key: %w", err)
		}
		// Load the contracts addresses into the contracts object
		if err := contracts.LoadContracts(contractsAddresses); err != nil {
			return nil, nil, fmt.Errorf("failed to load contracts addresses: %w", err)
		}
		log.Infow("contracts deployed and loaded",
			"chainId", contracts.ChainID,
			"addresses", contractsAddresses)
	} else {
		// Set the private key for the sequencer
		if err := contracts.SetAccountPrivateKey(util.TrimHex(privKey)); err != nil {
			return nil, nil, fmt.Errorf("failed to set account private key: %w", err)
		}
		// Create the contracts object with the addresses from the environment
		if err := contracts.LoadContracts(&web3.Addresses{
			OrganizationRegistry:      common.HexToAddress(orgRegistryAddr),
			ProcessRegistry:           common.HexToAddress(processRegistryAddr),
			ResultsZKVerifier:         common.HexToAddress(resultsZKVerifierAddr),
			StateTransitionZKVerifier: common.HexToAddress(stateTransitionZKVerifierAddr),
		}); err != nil {
			return nil, nil, fmt.Errorf("failed to load contracts addresses: %w", err)
		}
	}

	// Set contracts ABIs
	contracts.ContractABIs = &web3.ContractABIs{}
	contracts.ContractABIs.ProcessRegistry, err = contracts.ProcessRegistryABI()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get process registry ABI: %w", err)
	}
	contracts.ContractABIs.OrganizationRegistry, err = contracts.OrganizationRegistryABI()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get organization registry ABI: %w", err)
	}
	contracts.ContractABIs.StateTransitionZKVerifier, err = contracts.StateTransitionVerifierABI()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get state transition verifier ABI: %w", err)
	}
	contracts.ContractABIs.ResultsZKVerifier, err = contracts.ResultsVerifierABI()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get results verifier ABI: %w", err)
	}
	// Return the contracts object
	return contracts, srv, nil
}
