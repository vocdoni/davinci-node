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
	"testing"
	"time"

	"github.com/consensys/gnark/logger"
	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	qt "github.com/frankban/quicktest"
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
// contracts from the environment variables. It returns the contracts object.
func setupWeb3(t *testing.T, ctx context.Context) *web3.Contracts {
	c := qt.New(t)
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
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to read go.mod file: %v", err))
			modFile, err := modfile.Parse("go.mod", modData, nil)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to parse go.mod file: %v", err))
			// get the commit hash from the replace directive
			for _, r := range modFile.Require {
				if r.Mod.Path != "github.com/vocdoni/davinci-contracts" {
					continue
				}
				versionParts := strings.Split(r.Mod.Version, "-")
				c.Assert(len(versionParts), qt.Equals, 3)
				composeEnv[contractsCommitHashEnvVarName] = versionParts[2]
				break
			}
		}

		log.Infow("deploying contracts in local environment",
			"commit", composeEnv[contractsCommitHashEnvVarName],
			"branch", composeEnv[contractsBranchNameEnvVarName])

		// Create docker-compose instance
		compose, err := tc.NewDockerCompose("docker/docker-compose.yml")
		c.Assert(err, qt.IsNil)
		t.Cleanup(func() {
			downCtx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()
			err := compose.Down(downCtx, tc.RemoveOrphans(true), tc.RemoveVolumes(true))
			c.Assert(err, qt.IsNil)
		})
		ctx2, cancel := context.WithCancel(ctx)
		t.Cleanup(cancel)
		// Start docker-compose
		log.Infow("starting Anvil docker compose", "gethPort", anvilPort)
		err = compose.WithEnv(composeEnv).Up(ctx2, tc.Wait(true), tc.RemoveOrphans(true))
		c.Assert(err, qt.IsNil)
		deployerCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
		t.Cleanup(cancel)
		// Get the enpoint of the deployer service
		deployerContainer, err := compose.ServiceContainer(deployerCtx, "deployer")
		c.Assert(err, qt.IsNil)
		deployerUrl, err = deployerContainer.Endpoint(deployerCtx, "http")
		c.Assert(err, qt.IsNil)
	}

	// Wait for the RPC to be ready
	err := web3.WaitReadyRPC(ctx, rpcUrl)
	c.Assert(err, qt.IsNil)

	// Initialize the contracts object
	contracts, err := web3.New([]string{rpcUrl}, "")
	c.Assert(err, qt.IsNil)

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
				t.Fatal("timeout waiting for contracts to be deployed")
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
				c.Assert(res.StatusCode, qt.Equals, http.StatusOK)
				defer func() {
					err := res.Body.Close()
					c.Assert(err, qt.IsNil)
				}()
				// Decode the response
				var deployerResp deployerResponse
				err = json.NewDecoder(res.Body).Decode(&deployerResp)
				c.Assert(err, qt.IsNil)
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
		c.Assert(err, qt.IsNil)
		// Load the contracts addresses into the contracts object
		err = contracts.LoadContracts(contractsAddresses)
		c.Assert(err, qt.IsNil)
		log.Infow("contracts deployed and loaded",
			"chainId", contracts.ChainID,
			"addresses", contractsAddresses)
	} else {
		// Set the private key for the sequencer
		err = contracts.SetAccountPrivateKey(util.TrimHex(privKey))
		c.Assert(err, qt.IsNil)
		// Create the contracts object with the addresses from the environment
		err = contracts.LoadContracts(&web3.Addresses{
			OrganizationRegistry:      common.HexToAddress(orgRegistryAddr),
			ProcessRegistry:           common.HexToAddress(processRegistryAddr),
			ResultsZKVerifier:         common.HexToAddress(resultsZKVerifierAddr),
			StateTransitionZKVerifier: common.HexToAddress(stateTransitionZKVerifierAddr),
		})
		c.Assert(err, qt.IsNil)
	}

	// Start the transaction manager
	err = contracts.StartTxManager(ctx)
	c.Assert(err, qt.IsNil)
	c.Cleanup(contracts.StopTxManager)
	// Set contracts ABIs
	contracts.ContractABIs = &web3.ContractABIs{}
	contracts.ContractABIs.ProcessRegistry, err = contracts.ProcessRegistryABI()
	c.Assert(err, qt.IsNil)
	contracts.ContractABIs.OrganizationRegistry, err = contracts.OrganizationRegistryABI()
	c.Assert(err, qt.IsNil)
	contracts.ContractABIs.StateTransitionZKVerifier, err = contracts.StateTransitionVerifierABI()
	c.Assert(err, qt.IsNil)
	contracts.ContractABIs.ResultsZKVerifier, err = contracts.ResultsVerifierABI()
	c.Assert(err, qt.IsNil)
	// Return the contracts object
	return contracts
}

// NewTestClient creates a new API client for testing.
func NewTestClient(port int) (*client.HTTPclient, error) {
	return client.New(fmt.Sprintf("http://127.0.0.1:%d", port))
}

func NewTestService(
	t *testing.T,
	ctx context.Context,
	workerSecret string,
	workerTokenExpiration time.Duration,
	workerTimeout time.Duration,
	banRules *workers.WorkerBanRules,
) *Services {
	// Initialize the web3 contracts
	contracts := setupWeb3(t, ctx)

	kv, err := metadb.New(db.TypePebble, t.TempDir())
	qt.Assert(t, err, qt.IsNil)
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
		log.Fatal(err)
	}
	services.Sequencer = vp.Sequencer

	// Start sequencer batch time window
	services.Sequencer.SetBatchTimeWindow(defaultBatchTimeWindow)

	if os.Getenv("DEBUG") != "" && os.Getenv("DEBUG") != "false" {
		logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
		// Create a debug prover that will debug circuit execution during testing
		services.Sequencer.SetProver(sequencer.NewDebugProver(t))
	} else {
		t.Log("Debug prover is disabled! Set DEBUG=true to enable it.")
	}

	// Start process monitor
	pm := service.NewProcessMonitor(contracts, stg, time.Second*2)
	if err := pm.Start(ctx); err != nil {
		log.Fatal(err)
	}
	// Start API service
	web3Conf := config.DavinciWeb3Config{
		ProcessRegistrySmartContract:      contracts.ContractsAddresses.ProcessRegistry.String(),
		OrganizationRegistrySmartContract: contracts.ContractsAddresses.OrganizationRegistry.String(),
		ResultsZKVerifier:                 contracts.ContractsAddresses.ResultsZKVerifier.String(),
		StateTransitionZKVerifier:         contracts.ContractsAddresses.StateTransitionZKVerifier.String(),
	}
	api, err := setupAPI(ctx, stg, workerSecret, workerTokenExpiration, workerTimeout, banRules, web3Conf)
	qt.Assert(t, err, qt.IsNil)
	services.API = api

	t.Cleanup(func() {
		api.Stop()
		pm.Stop()
		vp.Stop()
		stg.Close()
	})
	return services
}

func createCensus(c *qt.C, cli *client.HTTPclient, size int) ([]byte, []*api.CensusParticipant, []*ethereum.Signer) {
	// Generate random participants
	signers := []*ethereum.Signer{}
	censusParticipants := api.CensusParticipants{Participants: []*api.CensusParticipant{}}
	for range size {
		signer, err := ethereum.NewSigner()
		if err != nil {
			c.Fatalf("failed to generate signer: %v", err)
		}
		censusParticipants.Participants = append(censusParticipants.Participants, &api.CensusParticipant{
			Key:    signer.Address().Bytes(),
			Weight: new(types.BigInt).SetUint64(circuits.MockWeight),
		})
		signers = append(signers, signer)
	}

	if isCSPCensus() {
		eddsaCSP, err := csp.New(types.CensusOriginCSPEdDSABLS12377, []byte(testLocalCSPSeed))
		c.Assert(err, qt.IsNil)
		root := eddsaCSP.CensusRoot()
		c.Assert(root, qt.IsNotNil)
		return root.Root, censusParticipants.Participants, signers
	} else {
		// Create a new census in the sequencer
		body, code, err := cli.Request(http.MethodPost, nil, nil, api.NewCensusEndpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(code, qt.Equals, http.StatusOK)

		var resp api.NewCensus
		err = json.NewDecoder(bytes.NewReader(body)).Decode(&resp)
		c.Assert(err, qt.IsNil)
		// Add participants to census
		addEnpoint := api.EndpointWithParam(api.AddCensusParticipantsEndpoint, api.CensusURLParam, resp.Census.String())
		_, code, err = cli.Request(http.MethodPost, censusParticipants, nil, addEnpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(code, qt.Equals, http.StatusOK)

		// Get census root
		getRootEnpoint := api.EndpointWithParam(api.GetCensusRootEndpoint, api.CensusURLParam, resp.Census.String())
		body, code, err = cli.Request(http.MethodGet, nil, nil, getRootEnpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(code, qt.Equals, http.StatusOK)

		var rootResp types.CensusRoot
		err = json.NewDecoder(bytes.NewReader(body)).Decode(&rootResp)
		c.Assert(err, qt.IsNil)
		return rootResp.Root, censusParticipants.Participants, signers
	}
}

func generateCensusProof(c *qt.C, cli *client.HTTPclient, root, pid, key []byte) *types.CensusProof {
	if isCSPCensus() {
		eddsaCSP, err := csp.New(types.CensusOriginCSPEdDSABLS12377, []byte(testLocalCSPSeed))
		c.Assert(err, qt.IsNil)
		processID := new(types.ProcessID).SetBytes(pid)
		cspProof, err := eddsaCSP.GenerateProof(processID, common.BytesToAddress(key))
		c.Assert(err, qt.IsNil)
		cspProof.Weight = new(types.BigInt).SetUint64(circuits.MockWeight)
		return cspProof
	} else {
		// Get proof for the key
		getProofEnpoint := api.EndpointWithParam(api.GetCensusProofEndpoint, api.CensusURLParam, hex.EncodeToString(root))
		body, code, err := cli.Request(http.MethodGet, nil, []string{"key", hex.EncodeToString(key)}, getProofEnpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(code, qt.Equals, http.StatusOK)

		var proof types.CensusProof
		err = json.NewDecoder(bytes.NewReader(body)).Decode(&proof)
		c.Assert(err, qt.IsNil)

		return &proof
	}
}

func createOrganization(c *qt.C, contracts *web3.Contracts) common.Address {
	orgAddr := contracts.AccountAddress()
	txHash, err := contracts.CreateOrganization(orgAddr, &types.OrganizationInfo{
		Name:        fmt.Sprintf("Vocdoni test %x", orgAddr[:4]),
		MetadataURI: "https://vocdoni.io",
	})
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to create organization: %v", err))

	err = contracts.WaitTx(txHash, time.Second*30)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to wait for organization creation transaction: %v", err))
	return orgAddr
}

func createProcessInSequencer(
	c *qt.C,
	contracts *web3.Contracts,
	cli *client.HTTPclient,
	censusOrigin types.CensusOrigin,
	censusRoot []byte,
	ballotMode *types.BallotMode,
) (*types.ProcessID, *types.EncryptionKey, *types.HexBytes) {
	// Geth the next process ID from the contracts
	processID, err := contracts.NextProcessID(contracts.AccountAddress())
	c.Assert(err, qt.IsNil)

	// Sign the process creation request
	signature, err := contracts.SignMessage(fmt.Appendf(nil, types.NewProcessMessageToSign, processID.String()))
	c.Assert(err, qt.IsNil)

	process := &types.ProcessSetup{
		ProcessID:    processID.Marshal(),
		CensusOrigin: censusOrigin,
		CensusRoot:   censusRoot,
		BallotMode:   ballotMode,
		Signature:    signature,
	}

	body, code, err := cli.Request(http.MethodPost, process, nil, api.ProcessesEndpoint)
	c.Assert(err, qt.IsNil)
	c.Assert(code, qt.Equals, http.StatusOK, qt.Commentf("response body %s", string(body)))

	var resp types.ProcessSetupResponse
	err = json.NewDecoder(bytes.NewReader(body)).Decode(&resp)
	c.Assert(err, qt.IsNil)
	c.Assert(resp.ProcessID, qt.Not(qt.IsNil))
	c.Assert(resp.EncryptionPubKey[0], qt.Not(qt.IsNil))
	c.Assert(resp.EncryptionPubKey[1], qt.Not(qt.IsNil))

	encryptionKeys := &types.EncryptionKey{
		X: resp.EncryptionPubKey[0],
		Y: resp.EncryptionPubKey[1],
	}
	return processID, encryptionKeys, &resp.StateRoot
}

func createProcessInContracts(c *qt.C, contracts *web3.Contracts,
	censusOrigin types.CensusOrigin,
	censusRoot []byte,
	ballotMode *types.BallotMode,
	encryptionKey *types.EncryptionKey,
	stateRoot *types.HexBytes,
	duration ...time.Duration,
) *types.ProcessID {
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
	c.Assert(err, qt.IsNil)

	err = contracts.WaitTx(*txHash, time.Second*15)
	c.Assert(err, qt.IsNil)

	return pid
}

func createVote(c *qt.C, pid *types.ProcessID, bm *types.BallotMode, encKey *types.EncryptionKey, privKey *ethereum.Signer, k *big.Int, fields []*types.BigInt) api.Vote {
	var err error
	// emulate user inputs
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	if k == nil {
		k, err = elgamal.RandK()
		c.Assert(err, qt.IsNil)
	}
	c.Logf("creating vote for address %s with fields %v", address.Hex(), fields)
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
	c.Assert(err, qt.IsNil)
	// encode the inputs to json
	encodedCircomInputs, err := json.Marshal(wasmResult.CircomInputs)
	c.Assert(err, qt.IsNil)
	// generate the proof using the circom circuit
	rawProof, pubInputs, err := ballotprooftest.CompileAndGenerateProofForTest(encodedCircomInputs)
	c.Assert(err, qt.IsNil)
	// convert the proof to gnark format
	circomProof, _, err := circomgnark.UnmarshalCircom(rawProof, pubInputs)
	c.Assert(err, qt.IsNil)
	// sign the hash of the circuit inputs
	signature, err := ballotprooftest.SignECDSAForTest(privKey, wasmResult.VoteID)
	c.Assert(err, qt.IsNil)
	// return the vote ready to be sent to the sequencer
	return api.Vote{
		ProcessID:        wasmResult.ProcessID,
		Address:          wasmInputs.Address,
		VoteID:           wasmResult.VoteID,
		Ballot:           wasmResult.Ballot,
		BallotProof:      circomProof,
		BallotInputsHash: wasmResult.BallotInputsHash,
		Signature:        signature.Bytes(),
	}
}

func createVoteWithRandomFields(c *qt.C, pid *types.ProcessID, bm *types.BallotMode, encKey *types.EncryptionKey, privKey *ethereum.Signer, k *big.Int) api.Vote {
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
	return createVote(c, pid, bm, encKey, privKey, k, fields)
}

func createVoteFromInvalidVoter(c *qt.C, pid *types.ProcessID, bm *types.BallotMode, encKey *types.EncryptionKey) api.Vote {
	privKey, err := ethereum.NewSigner()
	if err != nil {
		c.Fatalf("failed to generate signer: %v", err)
	}
	// emulate user inputs
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	k, err := elgamal.RandK()
	c.Assert(err, qt.IsNil)
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
	c.Assert(err, qt.IsNil)
	// encode the inputs to json
	encodedCircomInputs, err := json.Marshal(wasmResult.CircomInputs)
	c.Assert(err, qt.IsNil)
	// generate the proof using the circom circuit
	rawProof, pubInputs, err := ballotprooftest.CompileAndGenerateProofForTest(encodedCircomInputs)
	c.Assert(err, qt.IsNil)
	// convert the proof to gnark format
	circomProof, _, err := circomgnark.UnmarshalCircom(rawProof, pubInputs)
	c.Assert(err, qt.IsNil)
	// sign the hash of the circuit inputs
	signature, err := ballotprooftest.SignECDSAForTest(privKey, wasmResult.VoteID)
	c.Assert(err, qt.IsNil)
	// return the vote ready to be sent to the sequencer
	return api.Vote{
		ProcessID:        wasmResult.ProcessID,
		Address:          wasmInputs.Address,
		Ballot:           wasmResult.Ballot,
		BallotProof:      circomProof,
		BallotInputsHash: wasmResult.BallotInputsHash,
		Signature:        signature.Bytes(),
		VoteID:           wasmResult.VoteID,
	}
}

func checkVoteStatus(t *testing.T, cli *client.HTTPclient, pid *types.ProcessID, voteIDs []types.HexBytes, expectedStatus string) (bool, []types.HexBytes) {
	c := qt.New(t)
	// Check vote status and return whether all votes have the expected status
	txt := strings.Builder{}
	txt.WriteString(fmt.Sprintf("Vote status (expecting %s): ", expectedStatus))
	allExpectedStatus := true

	failed := []types.HexBytes{}
	// Check status for each vote
	for i, voteID := range voteIDs {
		// Construct the status endpoint URL
		statusEndpoint := api.EndpointWithParam(
			api.EndpointWithParam(api.VoteStatusEndpoint,
				api.ProcessURLParam, pid.String()),
			api.VoteStatusVoteIDParam, voteID.String())

		// Make the request to get the vote status
		body, statusCode, err := cli.Request("GET", nil, nil, statusEndpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(statusCode, qt.Equals, 200)

		// Parse the response body to get the status
		var statusResponse api.VoteStatusResponse
		err = json.NewDecoder(bytes.NewReader(body)).Decode(&statusResponse)
		c.Assert(err, qt.IsNil)

		// Verify the status is valid
		c.Assert(statusResponse.Status, qt.Not(qt.Equals), "")

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
		// Write to the string builder for logging
		txt.WriteString(fmt.Sprintf("#%d:%s ", i, statusResponse.Status))
	}

	// Log the vote status
	t.Log(txt.String())
	return allExpectedStatus, failed
}

func publishedVotes(t *testing.T, contracts *web3.Contracts, pid *types.ProcessID) int {
	c := qt.New(t)
	process, err := contracts.Process(pid.Marshal())
	c.Assert(err, qt.IsNil)
	if process == nil || process.VoteCount == nil {
		return 0
	}
	return int(process.VoteCount.MathBigInt().Int64())
}

func publishedOverwriteVotes(t *testing.T, contracts *web3.Contracts, pid *types.ProcessID) int {
	c := qt.New(t)
	process, err := contracts.Process(pid.Marshal())
	c.Assert(err, qt.IsNil)
	if process == nil || process.VoteOverwrittenCount == nil {
		return 0
	}
	return int(process.VoteOverwrittenCount.MathBigInt().Int64())
}

func finishProcessOnContract(t *testing.T, contracts *web3.Contracts, pid *types.ProcessID) {
	c := qt.New(t)
	txHash, err := contracts.SetProcessStatus(pid.Marshal(), types.ProcessStatusEnded)
	c.Assert(err, qt.IsNil)
	c.Assert(txHash, qt.IsNotNil)
	err = contracts.WaitTx(*txHash, time.Second*30)
	c.Assert(err, qt.IsNil)
	t.Logf("process %s finished successfully", pid.String())
}

func publishedResults(t *testing.T, contracts *web3.Contracts, pid *types.ProcessID) []*types.BigInt {
	c := qt.New(t)
	process, err := contracts.Process(pid.Marshal())
	c.Assert(err, qt.IsNil)
	if process == nil || process.Status != types.ProcessStatusResults || len(process.Result) == 0 {
		return nil
	}
	return process.Result
}
