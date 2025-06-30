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
	"path"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	qt "github.com/frankban/quicktest"
	tc "github.com/testcontainers/testcontainers-go/modules/compose"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/api/client"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	ballotprooftest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/sequencer"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
	"github.com/vocdoni/davinci-node/web3"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/metadb"
)

const (
	// first account private key created by anvil with default mnemonic
	testLocalAccountPrivKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	// envarionment variable names
	deployerServerPortEnvVarName      = "DEPLOYER_SERVER"                        // environment variable name for deployer server port
	contractsBranchNameEnvVarName     = "SEQUENCER_CONTRACTS_BRANCH"             // environment variable name for z-contracts branch
	privKeyEnvVarName                 = "SEQUENCER_PRIV_KEY"                     // environment variable name for private key
	rpcUrlEnvVarName                  = "SEQUENCER_RPC_URL"                      // environment variable name for RPC URL
	anvilPortEnvVarName               = "ANVIL_PORT_RPC_HTTP"                    // environment variable name for Anvil port
	orgRegistryEnvVarName             = "SEQUENCER_ORGANIZATION_REGISTRY"        // environment variable name for organization registry
	processRegistryEnvVarName         = "SEQUENCER_PROCESS_REGISTRY"             // environment variable name for process registry
	resultsVerifierEnvVarName         = "SEQUENCER_RESULTS_ZK_VERIFIER"          // environment variable name for results zk verifier
	stateTransitionVerifierEnvVarName = "SEQUENCER_STATE_TRANSITION_ZK_VERIFIER" // environment variable name for state transition zk verifier

)

// Services struct holds all test services
type Services struct {
	API       *service.APIService
	Sequencer *sequencer.Sequencer
	Storage   *storage.Storage
	Contracts *web3.Contracts
}

// setupAPI creates and starts a new API server for testing.
// It returns the server port.
func setupAPI(ctx context.Context, db *storage.Storage) (*service.APIService, error) {
	tmpPort := util.RandomInt(40000, 60000)

	api := service.NewAPI(db, "127.0.0.1", tmpPort, "test", false)
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
	contracts, err := web3.New([]string{rpcUrl})
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
		contractsCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
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

func NewTestService(t *testing.T, ctx context.Context) *Services {
	// Download circuit artifacts
	tempDir := "./test-artifacts"
	qt.Assert(t, os.MkdirAll(tempDir, 0755), qt.IsNil)
	err := service.DownloadArtifacts(time.Minute*20, path.Join(tempDir, "artifacts"))
	qt.Assert(t, err, qt.IsNil)

	// Initialize the web3 contracts
	contracts := setupWeb3(t, ctx)

	kv, err := metadb.New(db.TypePebble, t.TempDir())
	qt.Assert(t, err, qt.IsNil)
	stg := storage.New(kv)
	t.Cleanup(stg.Close)

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
	t.Cleanup(vp.Stop)
	services.Sequencer = vp.Sequencer

	// Start process monitor
	pm := service.NewProcessMonitor(contracts, stg, time.Second*2)
	if err := pm.Start(ctx); err != nil {
		log.Fatal(err)
	}
	t.Cleanup(pm.Stop)

	// Start API service
	api, err := setupAPI(ctx, stg)
	qt.Assert(t, err, qt.IsNil)
	t.Cleanup(api.Stop)
	services.API = api

	return services
}

func createCensus(cli *client.HTTPclient, size int) ([]byte, []*api.CensusParticipant, []*ethereum.Signer, error) {
	// Create a new census
	body, code, err := cli.Request(http.MethodPost, nil, nil, api.NewCensusEndpoint)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create census: %w", err)
	} else if code != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("failed to create census, status code: %d", code)
	}

	var resp api.NewCensus
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&resp); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode new census response: %w", err)
	}

	// Generate random participants
	signers := []*ethereum.Signer{}
	censusParticipants := api.CensusParticipants{Participants: []*api.CensusParticipant{}}
	for range size {
		signer, err := ethereum.NewSigner()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to create new signer: %w", err)
		}
		censusParticipants.Participants = append(censusParticipants.Participants, &api.CensusParticipant{
			Key:    signer.Address().Bytes(),
			Weight: new(types.BigInt).SetUint64(circuits.MockWeight),
		})
		signers = append(signers, signer)
	}

	// Add participants to census
	addEnpoint := api.EndpointWithParam(api.AddCensusParticipantsEndpoint, api.CensusURLParam, resp.Census.String())
	_, code, err = cli.Request(http.MethodPost, censusParticipants, nil, addEnpoint)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to add census participants: %w", err)
	} else if code != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("failed to add census participants, status code: %d", code)
	}

	// Get census root
	getRootEnpoint := api.EndpointWithParam(api.GetCensusRootEndpoint, api.CensusURLParam, resp.Census.String())
	body, code, err = cli.Request(http.MethodGet, nil, nil, getRootEnpoint)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get census root: %w", err)
	} else if code != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("failed to get census root, status code: %d", code)
	}

	var rootResp api.CensusRoot
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&rootResp); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode census root response: %w", err)
	}
	return rootResp.Root, censusParticipants.Participants, signers, err
}

func generateCensusProof(cli *client.HTTPclient, root []byte, key []byte) (*types.CensusProof, error) {
	// Get proof for the key
	getProofEnpoint := api.EndpointWithParam(api.GetCensusProofEndpoint, api.CensusURLParam, hex.EncodeToString(root))
	body, code, err := cli.Request(http.MethodGet, nil, []string{"key", hex.EncodeToString(key)}, getProofEnpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get census proof: %w", err)
	} else if code != http.StatusOK {
		return nil, fmt.Errorf("failed to get census proof, status code: %d", code)
	}

	var proof types.CensusProof
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&proof); err != nil {
		return nil, fmt.Errorf("failed to decode census proof response: %w", err)
	}
	return &proof, nil
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
	return orgAddr, contracts.WaitTx(txHash, time.Second*30)
}

func createProcess(contracts *web3.Contracts, cli *client.HTTPclient, censusRoot []byte, ballotMode types.BallotMode) (*types.ProcessID, *types.EncryptionKey, error) {
	// Geth the next process ID from the contracts
	processID, err := contracts.NextProcessID(contracts.AccountAddress())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get next process ID: %w", err)
	}

	// Sign the process creation request
	signature, err := contracts.SignMessage(fmt.Appendf(nil, types.NewProcessMessageToSign, processID.String()))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to sign process creation message: %w", err)
	}
	// Create the process setup request
	process := &types.ProcessSetup{
		ProcessID:  processID.Marshal(),
		CensusRoot: censusRoot,
		BallotMode: &ballotMode,
		Signature:  signature,
	}
	body, code, err := cli.Request(http.MethodPost, process, nil, api.ProcessesEndpoint)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create process: %w", err)
	} else if code != http.StatusOK {
		return nil, nil, fmt.Errorf("failed to create process, status code: %d", code)
	}
	var resp types.ProcessSetupResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&resp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode process setup response: %w", err)
	}
	encryptionKeys := &types.EncryptionKey{
		X: resp.EncryptionPubKey[0],
		Y: resp.EncryptionPubKey[1],
	}
	pid, txHash, err := contracts.CreateProcess(&types.Process{
		Status:         0,
		OrganizationId: contracts.AccountAddress(),
		EncryptionKey:  encryptionKeys,
		StateRoot:      resp.StateRoot.BigInt(),
		StartTime:      time.Now().Add(1 * time.Minute),
		Duration:       time.Hour,
		MetadataURI:    "https://example.com/metadata",
		BallotMode:     &ballotMode,
		Census: &types.Census{
			CensusRoot:   censusRoot,
			MaxVotes:     new(types.BigInt).SetUint64(1000),
			CensusURI:    "https://example.com/census",
			CensusOrigin: 0,
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create process: %w", err)
	}

	if err := contracts.WaitTx(*txHash, time.Second*15); err != nil {
		return nil, nil, fmt.Errorf("failed to wait for process creation transaction: %w", err)
	}

	return pid, encryptionKeys, nil
}

func createVote(pid *types.ProcessID, bm *types.BallotMode, encKey *types.EncryptionKey, privKey *ethereum.Signer, k *big.Int) (api.Vote, *big.Int, error) {
	var err error
	// emulate user inputs
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	if k == nil {
		k, err = elgamal.RandK()
		if err != nil {
			return api.Vote{}, nil, fmt.Errorf("failed to generate random k: %w", err)
		}
	}
	// generate random ballot fields
	randFields := ballotprooftest.GenBallotFieldsForTest(
		int(bm.MaxCount),
		int(bm.MaxValue.MathBigInt().Int64()),
		int(bm.MinValue.MathBigInt().Int64()),
		bm.ForceUniqueness)
	// cast fields to types.BigInt
	fields := []*types.BigInt{}
	for _, f := range randFields {
		fields = append(fields, (*types.BigInt)(f))
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
		return api.Vote{}, nil, fmt.Errorf("failed to generate ballot proof inputs: %w", err)
	}
	// encode the inputs to json
	encodedCircomInputs, err := json.Marshal(wasmResult.CircomInputs)
	if err != nil {
		return api.Vote{}, nil, fmt.Errorf("failed to encode circom inputs: %w", err)
	}
	// generate the proof using the circom circuit
	rawProof, pubInputs, err := ballotprooftest.CompileAndGenerateProofForTest(encodedCircomInputs)
	if err != nil {
		return api.Vote{}, nil, fmt.Errorf("failed to generate ballot proof: %w", err)
	}
	// convert the proof to gnark format
	circomProof, _, err := circuits.Circom2GnarkProof(rawProof, pubInputs)
	if err != nil {
		return api.Vote{}, nil, fmt.Errorf("failed to convert circom proof to gnark proof: %w", err)
	}
	// sign the hash of the circuit inputs
	signature, err := ballotprooftest.SignECDSAForTest(privKey, wasmResult.VoteID)
	if err != nil {
		return api.Vote{}, nil, fmt.Errorf("failed to sign vote ID: %w", err)
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
	}, k, nil
}

func createVoteFromInvalidVoter(pid *types.ProcessID, bm *types.BallotMode, encKey *types.EncryptionKey) (api.Vote, error) {
	privKey, err := ethereum.NewSigner()
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate new signer: %w", err)
	}
	// emulate user inputs
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	k, err := elgamal.RandK()
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate random k: %w", err)
	}
	// generate random ballot fields
	randFields := ballotprooftest.GenBallotFieldsForTest(
		int(bm.MaxCount),
		int(bm.MaxValue.MathBigInt().Int64()),
		int(bm.MinValue.MathBigInt().Int64()),
		bm.ForceUniqueness)
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
		return api.Vote{}, fmt.Errorf("failed to encode circom inputs: %w", err)
	}
	// generate the proof using the circom circuit
	rawProof, pubInputs, err := ballotprooftest.CompileAndGenerateProofForTest(encodedCircomInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate ballot proof: %w", err)
	}
	// convert the proof to gnark format
	circomProof, _, err := circuits.Circom2GnarkProof(rawProof, pubInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to convert circom proof to gnark proof: %w", err)
	}
	// sign the hash of the circuit inputs
	signature, err := ballotprooftest.SignECDSAForTest(privKey, wasmResult.VoteID)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to sign vote ID: %w", err)
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
		if err != nil {
			return false, nil, fmt.Errorf("error getting vote status for %s: %w", voteID.String(), err)
		} else if statusCode != 200 {
			return false, nil, fmt.Errorf("unexpected status code %d for vote %s", statusCode, voteID.String())
		}

		// Parse the response body to get the status
		var statusResponse api.VoteStatusResponse
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&statusResponse); err != nil {
			return false, nil, fmt.Errorf("error decoding vote status response for %s: %w", voteID.String(), err)
		}

		// Verify the status is valid
		if statusResponse.Status == "" {
			return false, nil, fmt.Errorf("empty status for vote %s", voteID.String())
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
		// Write to the string builder for logging
		txt.WriteString(fmt.Sprintf("#%d:%s ", i, statusResponse.Status))
	}

	// Log the vote status
	return allExpectedStatus, failed, nil
}

func publishedVotes(contracts *web3.Contracts, pid *types.ProcessID) (int, error) {
	process, err := contracts.Process(pid.Marshal())
	if err != nil {
		return 0, fmt.Errorf("failed to get process %s: %w", pid.String(), err)
	}
	if process == nil || process.VoteCount == nil {
		return 0, fmt.Errorf("process %s not found or has no votes", pid.String())
	}
	return int(process.VoteCount.MathBigInt().Int64()), nil
}

func publishedOverwriteVotes(contracts *web3.Contracts, pid *types.ProcessID) (int, error) {
	process, err := contracts.Process(pid.Marshal())
	if err != nil {
		return 0, fmt.Errorf("failed to get process %s: %w", pid, err)
	}
	if process == nil || process.VoteOverwrittenCount == nil {
		return 0, fmt.Errorf("process %s not found or has no overwritten votes", pid.String())
	}
	return int(process.VoteOverwrittenCount.MathBigInt().Int64()), nil
}

func finishProcessOnContract(contracts *web3.Contracts, pid *types.ProcessID) error {
	txHash, err := contracts.SetProcessStatus(pid.Marshal(), types.ProcessStatusEnded)
	if err != nil {
		return fmt.Errorf("failed to set process status: %w", err)
	}
	if err := contracts.WaitTx(*txHash, time.Second*30); err != nil {
		return fmt.Errorf("failed to wait for process status tx: %w", err)
	}
	return nil
}

func publishedResults(contracts *web3.Contracts, pid *types.ProcessID) ([]*types.BigInt, error) {
	process, err := contracts.Process(pid.Marshal())
	if err != nil {
		return nil, fmt.Errorf("failed to get process %s: %w", pid.String(), err)
	}
	if process == nil || process.Status != types.ProcessStatusResults || len(process.Result) == 0 {
		return nil, fmt.Errorf("process %s is not in results status or has no results", pid.String())
	}
	return process.Result, nil
}
