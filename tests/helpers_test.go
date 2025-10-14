package tests

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/consensys/gnark/logger"
	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/rs/zerolog"
	tc "github.com/testcontainers/testcontainers-go/modules/compose"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/api/client"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	ballotprooftest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/sequencer"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
	"github.com/vocdoni/davinci-node/util/circomgnark"
	"github.com/vocdoni/davinci-node/web3"
	"github.com/vocdoni/davinci-node/web3/txmanager"
	"github.com/vocdoni/davinci-node/workers"
	"golang.org/x/mod/modfile"
)

const (
	// first account private key created by anvil with default mnemonic
	testLocalAccountPrivKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	testLocalCSPSeed        = "1f1e0cd27b4ecd1b71b6333790864ace2870222c"
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
	cspCensusEnvVarName               = "CSP_CENSUS"                             // environment variable name to select between csp or merkle tree census (by default merkle tree)
)

var defaultBatchTimeWindow = 120 * time.Second // default batch time window for sequencer

// Services struct holds all test services
type Services struct {
	API       *service.APIService
	Sequencer *sequencer.Sequencer
	Storage   *storage.Storage
	Contracts *web3.Contracts
}

func boolEnvVar(name string) bool {
	enabled := os.Getenv(name)
	return enabled == "1" || enabled == "true" || enabled == "TRUE"
}

func isCSPCensus() bool {
	cspCensusEnvVar := os.Getenv(cspCensusEnvVarName)
	return strings.ToLower(cspCensusEnvVar) == "true" || cspCensusEnvVar == "1"
}

func testCensusOrigin() types.CensusOrigin {
	if isCSPCensus() {
		return types.CensusOriginCSPEdDSABLS12377
	} else {
		return types.CensusOriginMerkleTree
	}
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
	tmpPort := util.RandomInt(40000, 60000)

	api := service.NewAPI(db, "127.0.0.1", tmpPort, "test", web3Conf, false)
	api.SetWorkerConfig(workerSeed, workerTokenExpiration, workerTimeout, banRules)
	if err := api.Start(ctx); err != nil {
		return nil, err
	}

	// Wait for the HTTP server to start
	time.Sleep(500 * time.Millisecond)
	return api, nil
}

// setupWeb3 sets up the web3 contracts for testing. It deploys the contracts
// if the environment variables are not set, if they are set it loads the
// contracts from the environment variables. It returns the contracts object
// and a cleanup function that should be called when done.
func setupWeb3(ctx context.Context) (*web3.Contracts, func(), error) {
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
				if len(versionParts) != 3 {
					return nil, nil, fmt.Errorf("invalid version parts length: expected 3, got %d", len(versionParts))
				}
				composeEnv[contractsCommitHashEnvVarName] = versionParts[2]
				break
			}
		}

		log.Infow("deploying contracts in local environment",
			"commit", composeEnv[contractsCommitHashEnvVarName],
			"branch", composeEnv[contractsBranchNameEnvVarName])

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
		err = contracts.SetAccountPrivateKey(util.TrimHex(testLocalAccountPrivKey))
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
			OrganizationRegistry:      common.HexToAddress(orgRegistryAddr),
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
	c.Assert(err, qt.IsNil)
	txm.Start(ctx)
	contracts.SetTxManager(txm)
	c.Cleanup(txm.Stop)
	// Set contracts ABIs
	contracts.ContractABIs = &web3.ContractABIs{}
	contracts.ContractABIs.ProcessRegistry, err = contracts.ProcessRegistryABI()
	if err != nil {
		cleanup() // Clean up what we've done so far
		return nil, nil, fmt.Errorf("failed to get process registry ABI: %w", err)
	}
	contracts.ContractABIs.OrganizationRegistry, err = contracts.OrganizationRegistryABI()
	if err != nil {
		cleanup() // Clean up what we've done so far
		return nil, nil, fmt.Errorf("failed to get organization registry ABI: %w", err)
	}
	contracts.ContractABIs.StateTransitionZKVerifier, err = contracts.StateTransitionVerifierABI()
	if err != nil {
		cleanup() // Clean up what we've done so far
		return nil, nil, fmt.Errorf("failed to get state transition verifier ABI: %w", err)
	}
	contracts.ContractABIs.ResultsZKVerifier, err = contracts.ResultsVerifierABI()
	if err != nil {
		cleanup() // Clean up what we've done so far
		return nil, nil, fmt.Errorf("failed to get results verifier ABI: %w", err)
	}
	// Return the contracts object and cleanup function
	return contracts, cleanup, nil
}

// NewTestClient creates a new API client for testing.
func NewTestClient(port int) (*client.HTTPclient, error) {
	return client.New(fmt.Sprintf("http://127.0.0.1:%d", port))
}

func NewTestService(
	ctx context.Context,
	tempDir string,
	workerSecret string,
	workerTokenExpiration time.Duration,
	workerTimeout time.Duration,
	banRules *workers.WorkerBanRules,
) (*Services, func(), error) {
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

	services := &Services{
		Storage:   stg,
		Contracts: contracts,
	}

	// Start sequencer service
	sequencer.AggregatorTickerInterval = time.Second * 2
	sequencer.NewProcessMonitorInterval = time.Second * 5
	vp := service.NewSequencer(stg, contracts, time.Second*30, nil)
	if err := vp.Start(ctx); err != nil {
		web3Cleanup() // Clean up web3 if sequencer fails to start
		return nil, nil, fmt.Errorf("failed to start sequencer: %w", err)
	}
	services.Sequencer = vp.Sequencer

	// Start sequencer batch time window
	services.Sequencer.SetBatchTimeWindow(defaultBatchTimeWindow)

	if os.Getenv("DEBUG") != "" && os.Getenv("DEBUG") != "false" {
		logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
		// Note: Debug prover is disabled when not in testing context
		log.Info("Debug prover is disabled in non-testing context")
	}

	// Start process monitor
	pm := service.NewProcessMonitor(contracts, stg, time.Second*2)
	if err := pm.Start(ctx); err != nil {
		vp.Stop()
		web3Cleanup() // Clean up web3 if process monitor fails to start
		return nil, nil, fmt.Errorf("failed to start process monitor: %w", err)
	}
	// Start API service
	web3Conf := config.DavinciWeb3Config{
		ProcessRegistrySmartContract:      contracts.ContractsAddresses.ProcessRegistry.String(),
		OrganizationRegistrySmartContract: contracts.ContractsAddresses.OrganizationRegistry.String(),
		ResultsZKVerifier:                 contracts.ContractsAddresses.ResultsZKVerifier.String(),
		StateTransitionZKVerifier:         contracts.ContractsAddresses.StateTransitionZKVerifier.String(),
	}
	api, err := setupAPI(ctx, stg, workerSecret, workerTokenExpiration, workerTimeout, banRules, web3Conf)
	if err != nil {
		pm.Stop()
		vp.Stop()
		web3Cleanup() // Clean up web3 if API fails to start
		return nil, nil, fmt.Errorf("failed to setup API: %w", err)
	}
	services.API = api

	// Create a combined cleanup function
	cleanup := func() {
		api.Stop()
		pm.Stop()
		vp.Stop()
		stg.Close()
		web3Cleanup()
	}

	return services, cleanup, nil
}

func createCensus(cli *client.HTTPclient, size int) ([]byte, []*api.CensusParticipant, []*ethereum.Signer, error) {
	// Generate random participants
	signers := []*ethereum.Signer{}
	censusParticipants := api.CensusParticipants{Participants: []*api.CensusParticipant{}}
	for range size {
		signer, err := ethereum.NewSigner()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to generate signer: %w", err)
		}
		censusParticipants.Participants = append(censusParticipants.Participants, &api.CensusParticipant{
			Key:    signer.Address().Bytes(),
			Weight: new(types.BigInt).SetUint64(circuits.MockWeight),
		})
		signers = append(signers, signer)
	}

	if isCSPCensus() {
		eddsaCSP, err := csp.New(types.CensusOriginCSPEdDSABLS12377, []byte(testLocalCSPSeed))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to create CSP: %w", err)
		}
		root := eddsaCSP.CensusRoot()
		if root == nil {
			return nil, nil, nil, fmt.Errorf("census root is nil")
		}
		return root.Root, censusParticipants.Participants, signers, nil
	} else {
		// Create a new census in the sequencer
		body, code, err := cli.Request(http.MethodPost, nil, nil, api.NewCensusEndpoint)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to create census: %w", err)
		}
		if code != http.StatusOK {
			return nil, nil, nil, fmt.Errorf("unexpected status code creating census: %d", code)
		}

		var resp api.NewCensus
		err = json.NewDecoder(bytes.NewReader(body)).Decode(&resp)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to decode census response: %w", err)
		}
		// Add participants to census
		addEnpoint := api.EndpointWithParam(api.AddCensusParticipantsEndpoint, api.CensusURLParam, resp.Census.String())
		_, code, err = cli.Request(http.MethodPost, censusParticipants, nil, addEnpoint)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to add census participants: %w", err)
		}
		if code != http.StatusOK {
			return nil, nil, nil, fmt.Errorf("unexpected status code adding participants: %d", code)
		}

		// Get census root
		getRootEnpoint := api.EndpointWithParam(api.GetCensusRootEndpoint, api.CensusURLParam, resp.Census.String())
		body, code, err = cli.Request(http.MethodGet, nil, nil, getRootEnpoint)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get census root: %w", err)
		}
		if code != http.StatusOK {
			return nil, nil, nil, fmt.Errorf("unexpected status code getting root: %d", code)
		}

		var rootResp types.CensusRoot
		err = json.NewDecoder(bytes.NewReader(body)).Decode(&rootResp)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to decode root response: %w", err)
		}
		return rootResp.Root, censusParticipants.Participants, signers, nil
	}
}

func generateCensusProof(cli *client.HTTPclient, root, pid, key []byte) (*types.CensusProof, error) {
	if isCSPCensus() {
		eddsaCSP, err := csp.New(types.CensusOriginCSPEdDSABLS12377, []byte(testLocalCSPSeed))
		if err != nil {
			return nil, fmt.Errorf("failed to create CSP: %w", err)
		}
		processID := new(types.ProcessID).SetBytes(pid)
		cspProof, err := eddsaCSP.GenerateProof(processID, common.BytesToAddress(key))
		if err != nil {
			return nil, fmt.Errorf("failed to generate CSP proof: %w", err)
		}
		cspProof.Weight = new(types.BigInt).SetUint64(circuits.MockWeight)
		return cspProof, nil
	} else {
		// Get proof for the key
		getProofEnpoint := api.EndpointWithParam(api.GetCensusProofEndpoint, api.CensusURLParam, hex.EncodeToString(root))
		body, code, err := cli.Request(http.MethodGet, nil, []string{"key", hex.EncodeToString(key)}, getProofEnpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to get census proof: %w", err)
		}
		if code != http.StatusOK {
			return nil, fmt.Errorf("unexpected status code getting proof: %d", code)
		}

		var proof types.CensusProof
		err = json.NewDecoder(bytes.NewReader(body)).Decode(&proof)
		if err != nil {
			return nil, fmt.Errorf("failed to decode proof response: %w", err)
		}

		return &proof, nil
	}
}

func createOrganization(contracts *web3.Contracts) (common.Address, error) {
	orgAddr := contracts.AccountAddress()
	txHash, err := contracts.CreateOrganization(orgAddr, &types.OrganizationInfo{
		Name:        fmt.Sprintf("Vocdoni test %x", orgAddr[:4]),
		MetadataURI: "https://vocdoni.io",
	})
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to create organization: %w", err)
	}

	err = contracts.WaitTx(txHash, time.Second*30)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to wait for organization creation transaction: %w", err)
	}
	return orgAddr, nil
}

func createProcessInSequencer(
	contracts *web3.Contracts,
	cli *client.HTTPclient,
	censusOrigin types.CensusOrigin,
	censusRoot []byte,
	ballotMode *types.BallotMode,
) (*types.ProcessID, *types.EncryptionKey, *types.HexBytes, error) {
	// Get the next process ID from the contracts
	processID, err := contracts.NextProcessID(contracts.AccountAddress())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get next process ID: %w", err)
	}

	// Sign the process creation request
	signature, err := contracts.SignMessage(fmt.Appendf(nil, types.NewProcessMessageToSign, processID.String()))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to sign message: %w", err)
	}

	process := &types.ProcessSetup{
		ProcessID:    processID.Marshal(),
		CensusOrigin: censusOrigin,
		CensusRoot:   censusRoot,
		BallotMode:   ballotMode,
		Signature:    signature,
	}

	body, code, err := cli.Request(http.MethodPost, process, nil, api.ProcessesEndpoint)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create process: %w", err)
	}
	if code != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("unexpected status code creating process: %d, body: %s", code, string(body))
	}

	var resp types.ProcessSetupResponse
	err = json.NewDecoder(bytes.NewReader(body)).Decode(&resp)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode process response: %w", err)
	}
	if resp.ProcessID == nil {
		return nil, nil, nil, fmt.Errorf("process ID is nil")
	}
	if resp.EncryptionPubKey[0] == nil || resp.EncryptionPubKey[1] == nil {
		return nil, nil, nil, fmt.Errorf("encryption public key is nil")
	}

	encryptionKeys := &types.EncryptionKey{
		X: resp.EncryptionPubKey[0],
		Y: resp.EncryptionPubKey[1],
	}
	return processID, encryptionKeys, &resp.StateRoot, nil
}

func createProcessInContracts(
	contracts *web3.Contracts,
	censusOrigin types.CensusOrigin,
	censusRoot []byte,
	ballotMode *types.BallotMode,
	encryptionKey *types.EncryptionKey,
	stateRoot *types.HexBytes,
	duration ...time.Duration,
) (*types.ProcessID, error) {
	finalDuration := time.Hour
	if len(duration) > 0 {
		finalDuration = duration[0]
	}

	pid, txHash, err := contracts.CreateProcess(&types.Process{
		Status:         0,
		OrganizationId: contracts.AccountAddress(),
		EncryptionKey:  encryptionKey,
		StateRoot:      stateRoot.BigInt(),
		StartTime:      time.Now().Add(1 * time.Minute),
		Duration:       finalDuration,
		MetadataURI:    "https://example.com/metadata",
		BallotMode:     ballotMode,
		Census: &types.Census{
			CensusRoot:   censusRoot,
			MaxVotes:     new(types.BigInt).SetUint64(1000),
			CensusURI:    "https://example.com/census",
			CensusOrigin: censusOrigin,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create process: %w", err)
	}

	err = contracts.WaitTx(*txHash, time.Second*15)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for process creation transaction: %w", err)
	}

	return pid, nil
}

func waitUntilProcessStarts(
	contracts *web3.Contracts,
	pid *types.ProcessID,
	timeout time.Duration,
) error {
	// Wait for the process to be in the expected status
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for process to start")
		case <-ticker.C:
			process, err := contracts.Process(pid.Marshal())
			if err != nil {
				log.Warnw("failed to get process", "error", err)
				continue
			}
			if process == nil {
				log.Warnw("process not found", "processID", pid.String())
				continue
			}
			if process.Status == types.ProcessStatusReady && time.Now().After(process.StartTime) {
				return nil
			}
		}
	}
}

func createVote(pid *types.ProcessID, bm *types.BallotMode, encKey *types.EncryptionKey, privKey *ethereum.Signer, k *big.Int, fields []*types.BigInt) (api.Vote, error) {
	var err error
	// emulate user inputs
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	if k == nil {
		k, err = elgamal.RandK()
		if err != nil {
			return api.Vote{}, fmt.Errorf("failed to generate random k: %w", err)
		}
	}
	// compose wasm inputs
	wasmInputs := &ballotproof.BallotProofInputs{
		Address:   address.Bytes(),
		ProcessID: pid.Marshal(),
		EncryptionKey: []*types.BigInt{
			(*types.BigInt)(encKey.X),
			(*types.BigInt)(encKey.Y),
		},
		K:           (*types.BigInt)(k),
		BallotMode:  bm,
		Weight:      (*types.BigInt)(new(big.Int).SetUint64(circuits.MockWeight)),
		FieldValues: fields,
	}
	// generate the inputs for the ballot proof circuit
	wasmResult, err := ballotproof.GenerateBallotProofInputs(wasmInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate ballot proof inputs: %w", err)
	}
	// encode the inputs to json
	encodedCircomInputs, err := json.Marshal(wasmResult.CircomInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to marshal circom inputs: %w", err)
	}
	// generate the proof using the circom circuit
	rawProof, pubInputs, err := ballotprooftest.CompileAndGenerateProofForTest(encodedCircomInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to compile and generate proof: %w", err)
	}
	// convert the proof to gnark format
	circomProof, _, err := circomgnark.UnmarshalCircom(rawProof, pubInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to unmarshal circom proof: %w", err)
	}
	// sign the hash of the circuit inputs
	signature, err := ballotprooftest.SignECDSAForTest(privKey, wasmResult.VoteID)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to sign ECDSA: %w", err)
	}
	// return the vote ready to be sent to the sequencer
	return api.Vote{
		ProcessID:        wasmResult.ProcessID,
		Address:          wasmInputs.Address,
		VoteID:           wasmResult.VoteID,
		Ballot:           wasmResult.Ballot,
		BallotProof:      circomProof,
		BallotInputsHash: wasmResult.BallotInputsHash,
		Signature:        signature.Bytes(),
	}, nil
}

func createVoteWithRandomFields(pid *types.ProcessID, bm *types.BallotMode, encKey *types.EncryptionKey, privKey *ethereum.Signer, k *big.Int) (api.Vote, error) {
	// generate random ballot fields
	randFields := ballotprooftest.GenBallotFieldsForTest(
		int(bm.NumFields),
		int(bm.MaxValue.MathBigInt().Int64()),
		int(bm.MinValue.MathBigInt().Int64()),
		bm.UniqueValues)
	// cast fields to types.BigInt
	fields := []*types.BigInt{}
	for _, f := range randFields {
		fields = append(fields, (*types.BigInt)(f))
	}
	return createVote(pid, bm, encKey, privKey, k, fields)
}

func createVoteFromInvalidVoter(pid *types.ProcessID, bm *types.BallotMode, encKey *types.EncryptionKey) (api.Vote, error) {
	privKey, err := ethereum.NewSigner()
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate signer: %w", err)
	}
	// emulate user inputs
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	k, err := elgamal.RandK()
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate random k: %w", err)
	}
	// generate random ballot fields
	randFields := ballotprooftest.GenBallotFieldsForTest(
		int(bm.NumFields),
		int(bm.MaxValue.MathBigInt().Int64()),
		int(bm.MinValue.MathBigInt().Int64()),
		bm.UniqueValues)
	// compose wasm inputs
	wasmInputs := &ballotproof.BallotProofInputs{
		Address:       address.Bytes(),
		ProcessID:     pid.Marshal(),
		EncryptionKey: []*types.BigInt{encKey.X, encKey.Y},
		K:             new(types.BigInt).SetBigInt(k),
		BallotMode:    bm,
		Weight:        new(types.BigInt).SetUint64(circuits.MockWeight),
		FieldValues:   randFields[:],
	}
	// generate the inputs for the ballot proof circuit
	wasmResult, err := ballotproof.GenerateBallotProofInputs(wasmInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate ballot proof inputs: %w", err)
	}
	// encode the inputs to json
	encodedCircomInputs, err := json.Marshal(wasmResult.CircomInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to marshal circom inputs: %w", err)
	}
	// generate the proof using the circom circuit
	rawProof, pubInputs, err := ballotprooftest.CompileAndGenerateProofForTest(encodedCircomInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to compile and generate proof: %w", err)
	}
	// convert the proof to gnark format
	circomProof, _, err := circomgnark.UnmarshalCircom(rawProof, pubInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to unmarshal circom proof: %w", err)
	}
	// sign the hash of the circuit inputs
	signature, err := ballotprooftest.SignECDSAForTest(privKey, wasmResult.VoteID)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to sign ECDSA: %w", err)
	}
	// return the vote ready to be sent to the sequencer
	return api.Vote{
		ProcessID:        wasmResult.ProcessID,
		Address:          wasmInputs.Address,
		Ballot:           wasmResult.Ballot,
		BallotProof:      circomProof,
		BallotInputsHash: wasmResult.BallotInputsHash,
		Signature:        signature.Bytes(),
		VoteID:           wasmResult.VoteID,
	}, nil
}

func checkVoteStatus(cli *client.HTTPclient, pid *types.ProcessID, voteIDs []types.HexBytes, expectedStatus string) (bool, []types.HexBytes, error) {
	// Check vote status and return whether all votes have the expected status
	allExpectedStatus := true
	failed := []types.HexBytes{}

	// Check status for each vote
	for _, voteID := range voteIDs {
		// Construct the status endpoint URL
		statusEndpoint := api.EndpointWithParam(
			api.EndpointWithParam(api.VoteStatusEndpoint,
				api.ProcessURLParam, pid.String()),
			api.VoteStatusVoteIDParam, voteID.String())

		// Make the request to get the vote status
		body, statusCode, err := cli.Request("GET", nil, nil, statusEndpoint)
		if err != nil {
			return false, nil, fmt.Errorf("failed to request vote status: %w", err)
		}
		if statusCode != 200 {
			return false, nil, fmt.Errorf("unexpected status code: %d", statusCode)
		}

		// Parse the response body to get the status
		var statusResponse api.VoteStatusResponse
		err = json.NewDecoder(bytes.NewReader(body)).Decode(&statusResponse)
		if err != nil {
			return false, nil, fmt.Errorf("failed to decode status response: %w", err)
		}

		// Verify the status is valid
		if statusResponse.Status == "" {
			return false, nil, fmt.Errorf("status is empty")
		}

		// Check if the vote has the expected status
		switch statusResponse.Status {
		case storage.VoteIDStatusName(storage.VoteIDStatusError):
			allExpectedStatus = allExpectedStatus && (expectedStatus == storage.VoteIDStatusName(storage.VoteIDStatusError))
			if expectedStatus != storage.VoteIDStatusName(storage.VoteIDStatusError) {
				failed = append(failed, voteID)
			}
		case expectedStatus:
			allExpectedStatus = allExpectedStatus && true
		default:
			allExpectedStatus = false
		}
	}

	return allExpectedStatus, failed, nil
}

func publishedVotes(contracts *web3.Contracts, pid *types.ProcessID) (int, error) {
	process, err := contracts.Process(pid.Marshal())
	if err != nil {
		return 0, fmt.Errorf("failed to get process: %w", err)
	}
	if process == nil || process.VoteCount == nil {
		return 0, nil
	}
	return int(process.VoteCount.MathBigInt().Int64()), nil
}

func publishedOverwriteVotes(contracts *web3.Contracts, pid *types.ProcessID) (int, error) {
	process, err := contracts.Process(pid.Marshal())
	if err != nil {
		return 0, fmt.Errorf("failed to get process: %w", err)
	}
	if process == nil || process.VoteOverwrittenCount == nil {
		return 0, nil
	}
	return int(process.VoteOverwrittenCount.MathBigInt().Int64()), nil
}

func finishProcessOnContract(contracts *web3.Contracts, pid *types.ProcessID) error {
	txHash, err := contracts.SetProcessStatus(pid.Marshal(), types.ProcessStatusEnded)
	if err != nil {
		return fmt.Errorf("failed to set process status: %w", err)
	}
	if txHash == nil {
		return fmt.Errorf("transaction hash is nil")
	}
	err = contracts.WaitTx(*txHash, time.Second*30)
	if err != nil {
		return fmt.Errorf("failed to wait for transaction: %w", err)
	}
	return nil
}

func publishedResults(contracts *web3.Contracts, pid *types.ProcessID) ([]*types.BigInt, error) {
	process, err := contracts.Process(pid.Marshal())
	if err != nil {
		return nil, fmt.Errorf("failed to get process: %w", err)
	}
	if process == nil || process.Status != types.ProcessStatusResults || len(process.Result) == 0 {
		return nil, nil
	}
	return process.Result, nil
}
