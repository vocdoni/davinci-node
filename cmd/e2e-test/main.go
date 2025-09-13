package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	flag "github.com/spf13/pflag"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/api/client"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	ballotprooftest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/sequencer"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
	"github.com/vocdoni/davinci-node/util/circomgnark"
	"github.com/vocdoni/davinci-node/web3"
	"github.com/vocdoni/davinci-node/web3/rpc/chainlist"
)

const (
	defaultNetwork       = "sep"
	defaultCAPI          = "https://ethereum-sepolia-beacon-api.publicnode.com"
	defaultSequencerHost = "0.0.0.0"
	defaultSequencerPort = 8080
)

var (
	defaultSequencerEndpoint = fmt.Sprintf("http://%s:%d", defaultSequencerHost, defaultSequencerPort)
	defaultContracts         = config.DefaultConfig[defaultNetwork]

	mockedBallotMode = types.BallotMode{
		NumFields:      circuits.MockNumFields,
		UniqueValues:   circuits.MockUniqueValues == 1,
		MaxValue:       new(types.BigInt).SetUint64(circuits.MockMaxValue),
		MinValue:       new(types.BigInt).SetUint64(circuits.MockMinValue),
		MaxValueSum:    new(types.BigInt).SetUint64(circuits.MockMaxValueSum),
		MinValueSum:    new(types.BigInt).SetUint64(circuits.MockMinValueSum),
		CostFromWeight: circuits.MockCostFromWeight == 1,
		CostExponent:   circuits.MockCostExponent,
	}
)

func main() {
	// define cli flags
	var (
		privKey                          = flag.String("privkey", "", "private key to use for the Ethereum account")
		web3rpcs                         = flag.StringSlice("web3rpcs", nil, "web3 rpc http endpoints")
		consensusAPI                     = flag.String("consensusAPI", defaultCAPI, "web3 consensus API http endpoint")
		organizationRegistryAddress      = flag.String("organizationRegistryAddress", defaultContracts.OrganizationRegistrySmartContract, "organization registry smart contract address")
		processRegistryAddress           = flag.String("processRegistryAddress", defaultContracts.ProcessRegistrySmartContract, "process registry smart contract address")
		stateTransitionZKVerifierAddress = flag.String("stateTransitionZKVerifierAddress", defaultContracts.StateTransitionZKVerifier, "state transition zk verifier smart contract address")
		resultsZKVerifierAddress         = flag.String("resultsZKVerifierAddress", defaultContracts.ResultsZKVerifier, "state transition zk verifier smart contract address")
		testTimeout                      = flag.Duration("timeout", 20*time.Minute, "timeout for the test")
		sequencerEndpoint                = flag.String("sequencerEndpoint", defaultSequencerEndpoint, "sequencer endpoint")
		voteCount                        = flag.Int("voteCount", 10, "number of votes to cast")
		voteSleepTime                    = flag.Duration("voteSleepTime", 10*time.Second, "time to sleep between votes")
		web3Network                      = flag.StringP("web3.network", "n", defaultNetwork, fmt.Sprintf("network to use %v", config.AvailableNetworks))
	)
	flag.Parse()
	log.Init("debug", "stdout", nil)

	// Create a context with the test timeout
	testCtx, cancel := context.WithTimeout(context.Background(), *testTimeout)
	defer cancel()

	// Check if the private key is provided
	if *privKey == "" {
		log.Error("private key is required")
		flag.Usage()
		return
	}

	// If no web3rpcs are provided, use the default ones from chainlist
	var err error
	if len(*web3rpcs) == 0 {
		if *web3rpcs, err = chainlist.EndpointList(*web3Network, 10); err != nil {
			log.Fatal(err)
		}
	}

	// If the web3Network is not the default one, use the default contracts for that network or the provided addresses
	if *web3Network != defaultNetwork {
		contractAddrs := config.DefaultConfig[*web3Network]
		if *organizationRegistryAddress == defaultContracts.OrganizationRegistrySmartContract {
			*organizationRegistryAddress = contractAddrs.OrganizationRegistrySmartContract
		}
		if *processRegistryAddress == defaultContracts.ProcessRegistrySmartContract {
			*processRegistryAddress = contractAddrs.ProcessRegistrySmartContract
		}
		if *stateTransitionZKVerifierAddress == defaultContracts.StateTransitionZKVerifier {
			*stateTransitionZKVerifierAddress = contractAddrs.StateTransitionZKVerifier
		}
		if *resultsZKVerifierAddress == defaultContracts.ResultsZKVerifier {
			*resultsZKVerifierAddress = contractAddrs.ResultsZKVerifier
		}
	}

	log.Infow("using web3 configuration",
		"network", *web3Network,
		"organizationRegistryAddress", *organizationRegistryAddress,
		"processRegistryAddress", *processRegistryAddress,
		"stateTransitionZKVerifierAddress", *stateTransitionZKVerifierAddress,
		"resultsZKVerifierAddress", *resultsZKVerifierAddress,
		"web3rpcs", *web3rpcs,
	)

	// Intance contracts with the provided web3rpcs
	contracts, err := web3.New(*web3rpcs, *consensusAPI)
	if err != nil {
		log.Fatal(err)
	}
	// Load contracts from the default config
	if err = contracts.LoadContracts(&web3.Addresses{
		OrganizationRegistry:      common.HexToAddress(*organizationRegistryAddress),
		ProcessRegistry:           common.HexToAddress(*processRegistryAddress),
		StateTransitionZKVerifier: common.HexToAddress(*stateTransitionZKVerifierAddress),
		ResultsZKVerifier:         common.HexToAddress(*resultsZKVerifierAddress),
	}); err != nil {
		log.Fatal(err)
	}
	// Add the web3rpcs to the contracts
	for i := range *web3rpcs {
		if err := contracts.AddWeb3Endpoint((*web3rpcs)[i]); err != nil {
			log.Warnw("failed to add endpoint", "rpc", (*web3rpcs)[i], "err", err)
		}
	}
	// Set the private key for the account
	if err := contracts.SetAccountPrivateKey(util.TrimHex(*privKey)); err != nil {
		log.Fatal(err)
	}
	log.Infow("contracts initialized", "chainId", contracts.ChainID)

	// If no sequencer endpoint is provided, start a local one
	if *sequencerEndpoint == defaultSequencerEndpoint {
		log.Infow("no remote sequencer endpoint provided, starting a local one...")
		// Start a local sequencer
		service := new(localService)
		if err := service.Start(testCtx, contracts, *web3Network); err != nil {
			log.Fatal(err)
		}
		defer service.Stop()
		log.Infow("local sequencer started", "endpoint", defaultSequencerEndpoint)
	}
	// Create a API client
	cli, err := client.New(*sequencerEndpoint)
	if err != nil {
		log.Fatal(err)
	}
	// Wait for the sequencer to be ready, make ping request until it responds
	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for isConnected := false; !isConnected; {
		select {
		case <-pingCtx.Done():
			log.Fatal("ping timeout")
		default:
			_, status, err := cli.Request(http.MethodGet, nil, nil, api.PingEndpoint)
			if err == nil && status == http.StatusOK {
				isConnected = true
				break
			}
			log.Warnw("failed to ping sequencer", "status", status, "err", err)
			time.Sleep(10 * time.Second)
		}
	}
	log.Info("connected to sequencer")

	// Create a new organization
	organizationAddr, err := createOrganization(contracts)
	if err != nil {
		log.Errorw(err, "failed to create organization")
		log.Warn("check if the organization is already created or the account has enough funds")
		return
	}
	log.Infow("organization ready", "address", organizationAddr.Hex())

	// Create a new census with numBallot participants
	censusRoot, signers, err := createCensus(cli, *voteCount)
	if err != nil {
		log.Errorw(err, "failed to create census")
	}
	log.Infow("census created",
		"root", hex.EncodeToString(censusRoot),
		"participants", len(signers))

	// Create a new process with mocked ballot mode
	pid, encryptionKey, err := createProcess(testCtx, contracts, cli, censusRoot, mockedBallotMode)
	if err != nil {
		log.Errorw(err, "failed to create process")
		return
	}
	log.Infow("process created", "pid", pid.String())

	// Generate votes for each participant and send them to the sequencer
	for i, signer := range signers {
		// Generate a vote for each participant
		vote, err := createVote(signer, pid, encryptionKey, &mockedBallotMode)
		if err != nil {
			log.Errorw(err, "failed to create vote")
			return
		}
		log.Infow("vote created", "vote", vote)

		// Generate a census proof for each participant
		vote.CensusProof, err = generateCensusProof(cli, censusRoot, vote.Address)
		if err != nil {
			log.Errorw(err, "failed to generate census proof")
			return
		}
		log.Infow("census proof generated", "proof", vote.CensusProof)

		// Send the vote to the sequencer
		voteID, err := sendVote(cli, vote)
		if err != nil {
			log.Errorw(err, "failed to send vote")
			return
		}
		log.Infow("vote sent",
			"voteID", voteID.String(),
			"currentVote", i+1,
			"totalVotes", *voteCount)

		// Wait the voteSleepTime before sending the next vote
		time.Sleep(*voteSleepTime)
	}

	// Wait for the votes to be registered in the smart contract
	log.Info("all votes sent, waiting for votes to be registered in smart contract...")
	newVotesCh := make(chan int)
	newVotesCtx, cancel := context.WithCancel(testCtx)
	defer cancel()
	go func() {
		for newVoteCount := range newVotesCh {
			log.Infow("vote count registered in smart contract", "voteCount", newVoteCount)
			// Check if the vote count is equal to the number of votes sent
			if newVoteCount >= *voteCount {
				cancel()
				break
			}
		}
	}()
	if err := listenSmartContractVotesCount(newVotesCtx, contracts, pid, newVotesCh); err != nil {
		log.Errorw(err, "failed to wait for votes to be registered in smart contract")
		return
	}
	log.Info("all votes registered in smart contract, finishing the process in the smart contract...")
	time.Sleep(1 * time.Second)
	// finish the process in the smart contract
	if err := finishProcessOnChain(contracts, pid); err != nil {
		log.Errorw(err, "failed to finish process in smart contract")
		return
	}
	log.Infow("process finished in smart contract", "pid", pid.String())
	// Wait for the process to be finished in the sequencer
	resultsCtx, cancel := context.WithTimeout(testCtx, 2*time.Minute)
	defer cancel()
	results, err := waitForOnChainResults(resultsCtx, contracts, pid)
	if err != nil {
		log.Errorw(err, "failed to wait for on-chain results")
		return
	}
	log.Infow("on-chain results received", "pid", pid.String(), "results", results)
}

type localService struct {
	sequencer      *service.SequencerService
	processMonitor *service.ProcessMonitor
	storage        *storage.Storage
	api            *service.APIService
}

func (s *localService) Start(ctx context.Context, contracts *web3.Contracts, network string) error {
	// Create storage with a in-memory database
	s.storage = storage.New(memdb.New())
	sequencer.AggregatorTickerInterval = time.Second * 2
	sequencer.NewProcessMonitorInterval = time.Second * 5
	// Monitor new processes from the contracts
	s.processMonitor = service.NewProcessMonitor(contracts, s.storage, time.Second*2)
	if err := s.processMonitor.Start(ctx); err != nil {
		return fmt.Errorf("failed to start process monitor: %v", err)
	}
	// Start sequencer service
	s.sequencer = service.NewSequencer(s.storage, contracts, time.Second*30, nil)
	if err := s.sequencer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sequencer: %v", err)
	}
	// Start API service
	s.api = service.NewAPI(s.storage, defaultSequencerHost, defaultSequencerPort, network, false)
	if err := s.api.Start(ctx); err != nil {
		return fmt.Errorf("failed to start API: %v", err)
	}
	return nil
}

func (s *localService) Stop() {
	if s.sequencer != nil {
		s.sequencer.Stop()
	}
	if s.processMonitor != nil {
		s.processMonitor.Stop()
	}
	if s.api != nil {
		s.api.Stop()
	}
	if s.storage != nil {
		s.storage.Close()
	}
}

func createOrganization(contracts *web3.Contracts) (common.Address, error) {
	orgAddr := contracts.AccountAddress()
	if _, err := contracts.Organization(orgAddr); err == nil {
		return orgAddr, nil // Organization already exists
	}
	// Create a new organization in the contracts
	txHash, err := contracts.CreateOrganization(orgAddr, &types.OrganizationInfo{
		Name:        fmt.Sprintf("Vocdoni test %x", orgAddr[:4]),
		MetadataURI: "https://vocdoni.io",
	})
	if err != nil {
		return common.Address{}, err
	}

	// Wait for the transaction to be mined
	if err := contracts.WaitTx(txHash, time.Second*30); err != nil {
		return common.Address{}, err
	}

	return orgAddr, nil
}

func createCensus(cli *client.HTTPclient, size int) ([]byte, []*ethereum.Signer, error) {
	// Request a new census
	body, code, err := cli.Request(http.MethodPost, nil, nil, api.NewCensusEndpoint)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to request new census: %v", err)
	} else if code != http.StatusOK {
		return nil, nil, fmt.Errorf("failed to request new census, status code: %d", code)
	}

	// Decode census response
	var resp api.NewCensus
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&resp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode census response: %v", err)
	}

	// Generate random participants
	signers := []*ethereum.Signer{}
	censusParticipants := api.CensusParticipants{Participants: []*api.CensusParticipant{}}
	for range size {
		signer, err := ethereum.NewSigner()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create signer: %v", err)
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
		return nil, nil, fmt.Errorf("failed to add participants to census: %v", err)
	} else if code != http.StatusOK {
		return nil, nil, fmt.Errorf("failed to add participants to census, status code: %d", code)
	}

	// Get census root
	getRootEnpoint := api.EndpointWithParam(api.GetCensusRootEndpoint, api.CensusURLParam, resp.Census.String())
	body, code, err = cli.Request(http.MethodGet, nil, nil, getRootEnpoint)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get census root: %v", err)
	} else if code != http.StatusOK {
		return nil, nil, fmt.Errorf("failed to get census root, status code: %d", code)
	}

	// Decode census root
	var rootResp api.CensusRoot
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&rootResp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode census root response: %v", err)
	}

	return rootResp.Root, signers, nil
}

func createProcess(
	ctx context.Context,
	contracts *web3.Contracts,
	cli *client.HTTPclient,
	censusRoot []byte,
	ballotMode types.BallotMode,
) (*types.ProcessID, *types.EncryptionKey, error) {
	// Create test process request

	processId, err := contracts.NextProcessID(contracts.AccountAddress())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get next process ID: %v", err)
	}

	// Sign the process creation request
	signature, err := contracts.SignMessage(fmt.Appendf(nil, types.NewProcessMessageToSign, processId.String()))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to sign process creation request: %v", err)
	}

	// Make the request to create the process
	process := &types.ProcessSetup{
		ProcessID:    processId.Marshal(),
		CensusRoot:   censusRoot,
		BallotMode:   &ballotMode,
		Signature:    signature,
		CensusOrigin: types.CensusOriginMerkleTree,
	}
	body, code, err := cli.Request(http.MethodPost, process, nil, api.ProcessesEndpoint)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create process: %v", err)
	} else if code != http.StatusOK {
		return nil, nil, fmt.Errorf("failed to create process, status code: %d", code)
	}

	// Decode process response
	var resp types.ProcessSetupResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&resp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode process response: %v", err)
	}
	encryptionKeys := &types.EncryptionKey{
		X: resp.EncryptionPubKey[0],
		Y: resp.EncryptionPubKey[1],
	}

	newProcess := &types.Process{
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
			CensusOrigin: types.CensusOriginMerkleTree,
		},
	}
	// Create process in the contracts
	pid, txHash, err := contracts.CreateProcess(newProcess)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create process in contracts: %v", err)
	}

	// Wait for the process creation transaction to be mined
	if err = contracts.WaitTx(*txHash, time.Second*15); err != nil {
		return nil, nil, fmt.Errorf("failed to wait for process creation tx: %v", err)
	}

	// Wait for the process to be registered in the sequencer
	processCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for processReady := false; !processReady; {
		select {
		case <-time.After(time.Second * 5):
			pBytes, status, err := cli.Request(http.MethodGet, nil, nil, api.EndpointWithParam(api.ProcessEndpoint, api.ProcessURLParam, pid.String()))
			if err == nil && status == http.StatusOK {
				proc := &api.ProcessResponse{}
				if err := json.Unmarshal(pBytes, proc); err != nil {
					return nil, nil, fmt.Errorf("failed to unmarshal process response: %v", err)
				}
				processReady = proc.IsAcceptingVotes
			}
		case <-processCtx.Done():
			return nil, nil, fmt.Errorf("process creation timeout: %v", processCtx.Err())
		}
	}
	return pid, encryptionKeys, nil
}

func createVote(
	privKey *ethereum.Signer,
	pid *types.ProcessID,
	encKey *types.EncryptionKey,
	bm *types.BallotMode,
) (api.Vote, error) {
	// Emulate user inputs
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	k, err := elgamal.RandK()
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate random k: %v", err)
	}

	// Generate random ballot fields
	randFields := ballotprooftest.GenBallotFieldsForTest(
		int(bm.NumFields),
		int(bm.MaxValue.MathBigInt().Int64()),
		int(bm.MinValue.MathBigInt().Int64()),
		bm.UniqueValues)

	// Cast fields to types.BigInt
	fields := []*types.BigInt{}
	for _, f := range randFields {
		fields = append(fields, (*types.BigInt)(f))
	}

	// Compose wasm inputs
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

	// Generate the inputs for the ballot proof circuit
	wasmResult, err := ballotproof.GenerateBallotProofInputs(wasmInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate ballot proof inputs: %v", err)
	}

	// Encode the inputs to json
	encodedCircomInputs, err := json.Marshal(wasmResult.CircomInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to encode circom inputs: %v", err)
	}

	// Generate the proof using the circom circuit
	rawProof, pubInputs, err := ballotprooftest.CompileAndGenerateProofForTest(encodedCircomInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate proof: %v", err)
	}

	// Convert the proof to gnark format
	circomProof, _, err := circomgnark.UnmarshalCircom(rawProof, pubInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to convert proof to gnark format: %v", err)
	}

	// Sign the hash of the circuit inputs
	signature, err := ballotprooftest.SignECDSAForTest(privKey, wasmResult.VoteID)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to sign vote: %v", err)
	}

	// Return the vote ready to be sent to the sequencer
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

func generateCensusProof(cli *client.HTTPclient, root []byte, key []byte) (types.CensusProof, error) {
	// Get proof for the key
	getProofEnpoint := api.EndpointWithParam(api.GetCensusProofEndpoint, api.CensusURLParam, hex.EncodeToString(root))
	body, code, err := cli.Request(http.MethodGet, nil, []string{"key", hex.EncodeToString(key)}, getProofEnpoint)
	if err != nil {
		return types.CensusProof{}, fmt.Errorf("failed to get census proof: %v", err)
	} else if code != http.StatusOK {
		return types.CensusProof{}, fmt.Errorf("failed to get census proof, status code: %d", code)
	}

	// Decode proof response
	var proof types.CensusProof
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&proof); err != nil {
		return types.CensusProof{}, fmt.Errorf("failed to decode census proof response: %v", err)
	}

	return proof, nil
}

func sendVote(cli *client.HTTPclient, vote api.Vote) (types.HexBytes, error) {
	// Make the request to cast the vote
	_, status, err := cli.Request(http.MethodPost, vote, nil, api.VotesEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to cast vote: %v", err)
	} else if status != http.StatusOK {
		return nil, fmt.Errorf("failed to cast vote, status code: %d", status)
	}
	return vote.VoteID, nil
}

func listenSmartContractVotesCount(
	ctx context.Context,
	contracts *web3.Contracts,
	pid *types.ProcessID,
	newVotes chan int,
) error {
	ticker := time.NewTicker(time.Second * 30)
	lastVotesCount := -1
	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				close(newVotes)
				return nil
			}
			return fmt.Errorf("process creation timeout: %v", ctx.Err())
		case <-ticker.C:
			process, err := contracts.Process(pid.Marshal())
			if err != nil {
				return fmt.Errorf("failed to get process: %v", err)
			}
			if process == nil {
				return fmt.Errorf("process not found")
			}
			// Get the vote count from the process
			var newVotesCount int
			if process.VoteCount != nil {
				newVotesCount = int(process.VoteCount.MathBigInt().Int64())
				log.Debugw("new vote count", "pid", pid.String(), "newVotesCount", newVotesCount)
			}
			if newVotesCount > lastVotesCount {
				lastVotesCount = newVotesCount
				newVotes <- newVotesCount
			}
		}
	}
}

func finishProcessOnChain(contracts *web3.Contracts, pid *types.ProcessID) error {
	finishTx, err := contracts.SetProcessStatus(pid.Marshal(), types.ProcessStatusEnded)
	if err != nil {
		return fmt.Errorf("failed to finish process: %v", err)
	}
	if err := contracts.WaitTx(*finishTx, time.Second*30); err != nil {
		return fmt.Errorf("failed to wait for process finish tx: %v", err)
	}
	return nil
}

func waitForOnChainResults(ctx context.Context, contracts *web3.Contracts, pid *types.ProcessID) ([]*types.BigInt, error) {
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			process, err := contracts.Process(pid.Marshal())
			if err != nil {
				return nil, fmt.Errorf("failed to get process: %v", err)
			}
			if process == nil {
				return nil, fmt.Errorf("process not found")
			}
			if process.Status == types.ProcessStatusResults {
				return process.Result, nil
			}
		case <-ctx.Done():
			return nil, fmt.Errorf("context canceled while waiting for results: %v", ctx.Err())
		}
	}
}
