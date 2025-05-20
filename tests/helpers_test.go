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

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	qt "github.com/frankban/quicktest"
	tc "github.com/testcontainers/testcontainers-go/modules/compose"
	"github.com/vocdoni/vocdoni-z-sandbox/api"
	"github.com/vocdoni/vocdoni-z-sandbox/api/client"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/ballotproof"
	ballotprooftest "github.com/vocdoni/vocdoni-z-sandbox/circuits/test/ballotproof"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/signatures/ethereum"
	"github.com/vocdoni/vocdoni-z-sandbox/finalizer"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/sequencer"
	"github.com/vocdoni/vocdoni-z-sandbox/service"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"github.com/vocdoni/vocdoni-z-sandbox/util"
	"github.com/vocdoni/vocdoni-z-sandbox/web3"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/metadb"
)

const (
	testLocalAccountPrivKey = "0cebebc37477f513cd8f946ffced46e368aa4f9430250ce4507851edbba86b20" // defined in docker/files/genesis.json
	// envarionment variable names
	privKeyEnvVarName         = "SEQUENCER_PRIV_KEY"              // environment variable name for private key
	rpcUrlEnvVarName          = "SEQUENCER_RPC_URL"               // environment variable name for RPC URL
	orgRegistryEnvVarName     = "SEQUENCER_ORGANIZATION_REGISTRY" // environment variable name for organization registry
	processRegistryEnvVarName = "SEQUENCER_PROCESS_REGISTRY"      // environment variable name for process registry
	resultsRegistryEnvVarName = "SEQUENCER_RESULTS_REGISTRY"      // environment variable name for results registry
	zkVerifierEnvVarName      = "SEQUENCER_ZK_VERIFIER"           // environment variable name for zk verifier

)

// Services struct holds all test services
type Services struct {
	API       *service.APIService
	Sequencer *sequencer.Sequencer
	Finalizer *finalizer.Finalizer
	Storage   *storage.Storage
	Contracts *web3.Contracts
}

// setupAPI creates and starts a new API server for testing.
// It returns the server port.
func setupAPI(ctx context.Context, db *storage.Storage) (*service.APIService, error) {
	tmpPort := util.RandomInt(40000, 60000)

	api := service.NewAPI(db, "127.0.0.1", tmpPort, "local")
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
	// Get the environment variables
	var (
		privKey             = os.Getenv(privKeyEnvVarName)
		rpcUrl              = os.Getenv(rpcUrlEnvVarName)
		orgRegistryAddr     = os.Getenv(orgRegistryEnvVarName)
		processRegistryAddr = os.Getenv(processRegistryEnvVarName)
		resultsRegistryAddr = os.Getenv(resultsRegistryEnvVarName)
		zkVerifierAddr      = os.Getenv(zkVerifierEnvVarName)
	)
	// Check if the environment variables are set to run the tests over local
	// geth node or remote blockchain environment
	localEnv := privKey == "" || rpcUrl == "" || orgRegistryAddr == "" ||
		processRegistryAddr == "" || resultsRegistryAddr == "" || zkVerifierAddr == ""
	if localEnv {
		// Generate a random port for geth HTTP RPC
		gethPort := util.RandomInt(10000, 20000)
		rpcUrl = fmt.Sprintf("http://localhost:%d", gethPort)
		// Set environment variables for docker-compose in the process environment
		composeEnv := make(map[string]string)
		composeEnv["GETH_PORT_8545"] = fmt.Sprintf("%d", gethPort)
		composeEnv["GETH_PORT_8546"] = fmt.Sprintf("%d", gethPort+1)
		composeEnv["GETH_PORT_8551"] = fmt.Sprintf("%d", gethPort+6)

		// Create docker-compose instance
		compose, err := tc.NewDockerCompose("docker/docker-compose.yml")
		qt.Assert(t, err, qt.IsNil)
		t.Cleanup(func() {
			err := compose.Down(ctx, tc.RemoveOrphans(true), tc.RemoveVolumes(true))
			qt.Assert(t, err, qt.IsNil)
		})
		ctx2, cancel := context.WithCancel(ctx)
		t.Cleanup(cancel)

		// Start docker-compose
		log.Infow("starting Geth docker compose", "gethPort", gethPort)
		err = compose.WithEnv(composeEnv).Up(ctx2, tc.Wait(true), tc.RemoveOrphans(true))
		qt.Assert(t, err, qt.IsNil)
	}

	// Wait for the RPC to be ready
	err := web3.WaitReadyRPC(ctx, rpcUrl)
	qt.Assert(t, err, qt.IsNil)

	// Initialize the contracts object
	contracts, err := web3.New([]string{rpcUrl})
	qt.Assert(t, err, qt.IsNil)

	// Define contracts addresses or deploy them
	if localEnv {
		// Deploy the contracts using the local geth node
		log.Infow("deploying contracts", "url", rpcUrl)
		contracts, err = web3.DeployContracts(rpcUrl, testLocalAccountPrivKey)
		qt.Assert(t, err, qt.IsNil)
		log.Infow("contracts deployed", "chainId", contracts.ChainID)
	} else {
		// Set the private key for the sequencer
		err = contracts.SetAccountPrivateKey(util.TrimHex(privKey))
		qt.Assert(t, err, qt.IsNil)
		// Create the contracts object with the addresses from the environment
		err = contracts.LoadContracts(&web3.Addresses{
			OrganizationRegistry: common.HexToAddress(orgRegistryAddr),
			ProcessRegistry:      common.HexToAddress(processRegistryAddr),
			ResultsRegistry:      common.HexToAddress(resultsRegistryAddr),
			ZKVerifier:           common.HexToAddress(zkVerifierAddr),
		})
		qt.Assert(t, err, qt.IsNil)
	}

	// Set contracts ABIs
	contracts.ContractABIs = &web3.ContractABIs{}
	contracts.ContractABIs.ProcessRegistry, err = contracts.ProcessRegistryABI()
	qt.Assert(t, err, qt.IsNil)
	contracts.ContractABIs.OrganizationRegistry, err = contracts.OrganizationRegistryABI()
	qt.Assert(t, err, qt.IsNil)
	contracts.ContractABIs.ZKVerifier, err = contracts.ZKVerifierABI()
	qt.Assert(t, err, qt.IsNil)
	// Return the contracts object
	return contracts
}

// NewTestClient creates a new API client for testing.
func NewTestClient(port int) (*client.HTTPclient, error) {
	return client.New(fmt.Sprintf("http://127.0.0.1:%d", port))
}

func NewTestService(t *testing.T, ctx context.Context) *Services {
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
	vp := service.NewSequencer(stg, contracts, time.Second*30)
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

	// Start finalizer service
	fin := service.NewFinalizer(stg, stg.StateDB(), time.Second*5)
	if err := fin.Start(ctx, time.Second*5); err != nil {
		log.Fatal(err)
	}
	t.Cleanup(fin.Stop)
	services.Finalizer = fin.Finalizer

	// Start API service
	api, err := setupAPI(ctx, stg)
	qt.Assert(t, err, qt.IsNil)
	t.Cleanup(api.Stop)
	services.API = api

	return services
}

func createCensus(c *qt.C, cli *client.HTTPclient, size int) ([]byte, []*api.CensusParticipant, []*ethereum.Signer) {
	// Create a new census
	body, code, err := cli.Request(http.MethodPost, nil, nil, api.NewCensusEndpoint)
	c.Assert(err, qt.IsNil)
	c.Assert(code, qt.Equals, http.StatusOK)

	var resp api.NewCensus
	err = json.NewDecoder(bytes.NewReader(body)).Decode(&resp)
	c.Assert(err, qt.IsNil)

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

	var rootResp api.CensusRoot
	err = json.NewDecoder(bytes.NewReader(body)).Decode(&rootResp)
	c.Assert(err, qt.IsNil)

	return rootResp.Root, censusParticipants.Participants, signers
}

func generateCensusProof(c *qt.C, cli *client.HTTPclient, root []byte, key []byte) *types.CensusProof {
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

func createOrganization(c *qt.C, contracts *web3.Contracts) common.Address {
	orgAddr := contracts.AccountAddress()
	txHash, err := contracts.CreateOrganization(orgAddr, &types.OrganizationInfo{
		Name:        fmt.Sprintf("Vocdoni test %x", orgAddr[:4]),
		MetadataURI: "https://vocdoni.io",
	})
	c.Assert(err, qt.IsNil)

	err = contracts.WaitTx(txHash, time.Second*30)
	c.Assert(err, qt.IsNil)
	return orgAddr
}

func createProcess(c *qt.C, contracts *web3.Contracts, cli *client.HTTPclient, censusRoot []byte, ballotMode types.BallotMode) (*types.ProcessID, *types.EncryptionKey) {
	// Create test process request
	nonce, err := contracts.AccountNonce()
	c.Assert(err, qt.IsNil)

	// Sign the process creation request
	signature, err := contracts.SignMessage([]byte(fmt.Sprintf("%d%d", contracts.ChainID, nonce)))
	c.Assert(err, qt.IsNil)

	process := &types.ProcessSetup{
		CensusRoot: censusRoot,
		BallotMode: &ballotMode,
		Nonce:      nonce,
		ChainID:    uint32(contracts.ChainID),
		Signature:  signature,
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
		X: (*big.Int)(&resp.EncryptionPubKey[0]),
		Y: (*big.Int)(&resp.EncryptionPubKey[1]),
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
	c.Assert(err, qt.IsNil)

	err = contracts.WaitTx(*txHash, time.Second*15)
	c.Assert(err, qt.IsNil)

	return pid, encryptionKeys
}

func createVote(c *qt.C, pid *types.ProcessID, bm *types.BallotMode, encKey *types.EncryptionKey, privKey *ethereum.Signer) api.Vote {
	// emulate user inputs
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	secret := util.RandomBytes(16)
	k, err := elgamal.RandK()
	c.Assert(err, qt.IsNil)
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
		Secret:    secret,
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
	circomProof, _, err := circuits.Circom2GnarkProof(rawProof, pubInputs)
	c.Assert(err, qt.IsNil)
	// sign the hash of the circuit inputs
	signature, err := ballotprooftest.SignECDSAForTest(privKey, wasmResult.VoteID)
	c.Assert(err, qt.IsNil)
	// return the vote ready to be sent to the sequencer
	return api.Vote{
		ProcessID:        wasmResult.ProccessID,
		Address:          wasmInputs.Address,
		Commitment:       wasmResult.Commitment,
		Nullifier:        wasmResult.Nullifier,
		Ballot:           wasmResult.Ballot,
		BallotProof:      circomProof,
		BallotInputsHash: wasmResult.BallotInputsHash,
		Signature:        signature.Bytes(),
	}
}

func checkProcessedVotes(t *testing.T, cli *client.HTTPclient, pid *types.ProcessID, voteIDs []types.HexBytes) bool {
	c := qt.New(t)
	// Check vote status and return whether all votes are processed
	txt := strings.Builder{}
	txt.WriteString("Vote status: ")
	allProcessed := true

	// Check status for each vote
	for i, voteID := range voteIDs {
		// Construct the status endpoint URL
		statusEndpoint := api.EndpointWithParam(
			api.EndpointWithParam(api.VoteStatusEndpoint,
				api.VoteStatusProcessIDParam, pid.String()),
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

		// Check if the vote is processed
		if statusResponse.Status != "processed" {
			allProcessed = false
		}

		// Write to the string builder for logging
		txt.WriteString(fmt.Sprintf("#%d:%s ", i, statusResponse.Status))
	}

	// Log the vote status
	t.Log(txt.String())
	return allProcessed
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
