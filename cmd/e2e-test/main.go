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
	"github.com/vocdoni/vocdoni-z-sandbox/api"
	"github.com/vocdoni/vocdoni-z-sandbox/api/client"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/ballotproof"
	ballotprooftest "github.com/vocdoni/vocdoni-z-sandbox/circuits/test/ballotproof"
	"github.com/vocdoni/vocdoni-z-sandbox/config"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/signatures/ethereum"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/sequencer"
	"github.com/vocdoni/vocdoni-z-sandbox/service"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"github.com/vocdoni/vocdoni-z-sandbox/util"
	"github.com/vocdoni/vocdoni-z-sandbox/web3"
	"github.com/vocdoni/vocdoni-z-sandbox/web3/rpc/chainlist"
)

const (
	defaultNetwork       = "sep"
	defaultSequencerHost = "0.0.0.0"
	defaultSequencerPort = 8080
)

var (
	defaultSequencerEndpoint = fmt.Sprintf("http://%s:%d", defaultSequencerHost, defaultSequencerPort)
	defaultContracts         = config.DefaultConfig[defaultNetwork]

	mockedBallotMode = types.BallotMode{
		MaxCount:        circuits.MockMaxCount,
		ForceUniqueness: circuits.MockForceUniqueness == 1,
		MaxValue:        new(types.BigInt).SetUint64(circuits.MockMaxValue),
		MinValue:        new(types.BigInt).SetUint64(circuits.MockMinValue),
		MaxTotalCost:    new(types.BigInt).SetUint64(circuits.MockMaxTotalCost),
		MinTotalCost:    new(types.BigInt).SetUint64(circuits.MockMinTotalCost),
		CostFromWeight:  circuits.MockCostFromWeight == 1,
		CostExponent:    circuits.MockCostExp,
	}
)

func main() {
	// define cli flags
	var (
		privKey                          = flag.String("privkey", "", "private key to use for the Ethereum account")
		web3rpcs                         = flag.StringSlice("web3rpcs", nil, "web3 rpc http endpoints")
		organizationRegistryAddress      = flag.String("organizationRegistryAddress", defaultContracts.OrganizationRegistrySmartContract, "organization registry smart contract address")
		processRegistryAddress           = flag.String("processRegistryAddress", defaultContracts.ProcessRegistrySmartContract, "process registry smart contract address")
		stateTransitionZKVerifierAddress = flag.String("stateTransitionZKVerifierAddress", defaultContracts.StateTransitionZKVerifier, "state transition zk verifier smart contract address")
		testTimeout                      = flag.Duration("timeout", 20*time.Minute, "timeout for the test")
		sequencerEndpoint                = flag.String("sequencerEndpoint", defaultSequencerEndpoint, "sequencer endpoint")
		createOrg                        = flag.Bool("createOrganization", true, "create a new organization")
		voteCount                        = flag.Int("voteCount", 10, "number of votes to cast")
		voteSleepTime                    = flag.Duration("voteSleepTime", 10*time.Second, "time to sleep between votes")
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
		if *web3rpcs, err = chainlist.EndpointList(defaultNetwork, 10); err != nil {
			log.Fatal(err)
		}
	}

	// Intance contracts with the provided web3rpcs
	contracts, err := web3.New(*web3rpcs)
	if err != nil {
		log.Fatal(err)
	}
	// Load contracts from the default config
	if err = contracts.LoadContracts(&web3.Addresses{
		OrganizationRegistry:      common.HexToAddress(*organizationRegistryAddress),
		ProcessRegistry:           common.HexToAddress(*processRegistryAddress),
		StateTransitionZKVerifier: common.HexToAddress(*stateTransitionZKVerifierAddress),
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
		if err := service.Start(testCtx, contracts); err != nil {
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
	if *createOrg {
		organizationAddr, err := createOrganization(contracts)
		if err != nil {
			log.Errorw(err, "failed to create organization")
			log.Warn("check if the organization is already created or the account has enough funds")
			return
		}
		log.Infow("organization created", "address", organizationAddr.Hex())
	} else {
		log.Infow("skipping organization creation")
	}

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
	log.Info("all votes registered in smart contract")
	time.Sleep(1 * time.Second)
}

type localService struct {
	sequencer      *service.SequencerService
	processMonitor *service.ProcessMonitor
	finalizer      *service.FinalizerService
	api            *service.APIService
}

func (s *localService) Start(ctx context.Context, contracts *web3.Contracts) error {
	// Create storage with a in-memory database
	stg := storage.New(memdb.New())
	sequencer.AggregatorTickerInterval = time.Second * 2
	sequencer.NewProcessMonitorInterval = time.Second * 5
	s.sequencer = service.NewSequencer(stg, contracts, time.Second*30)
	if err := s.sequencer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sequencer: %v", err)
	}
	// Monitor new processes from the contracts
	s.processMonitor = service.NewProcessMonitor(contracts, stg, time.Second*2)
	if err := s.processMonitor.Start(ctx); err != nil {
		return fmt.Errorf("failed to start process monitor: %v", err)
	}
	// Start finalizer service
	s.finalizer = service.NewFinalizer(stg, stg.StateDB(), time.Second*5)
	if err := s.finalizer.Start(ctx, time.Second*5); err != nil {
		return fmt.Errorf("failed to start finalizer: %v", err)
	}
	// Start API service
	s.api = service.NewAPI(stg, defaultSequencerHost, defaultSequencerPort, defaultNetwork)
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
	if s.finalizer != nil {
		s.finalizer.Stop()
	}
	if s.api != nil {
		s.api.Stop()
	}
}

func createOrganization(contracts *web3.Contracts) (common.Address, error) {
	orgAddr := contracts.AccountAddress()
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
	nonce, err := contracts.AccountNonce()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get account nonce: %v", err)
	}

	// Sign the process creation request
	signature, err := contracts.SignMessage(fmt.Appendf(nil, "%d%d", contracts.ChainID, nonce))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to sign process creation request: %v", err)
	}

	// Make the request to create the process
	process := &types.ProcessSetup{
		CensusRoot: censusRoot,
		BallotMode: &ballotMode,
		Nonce:      nonce,
		ChainID:    uint32(contracts.ChainID),
		Signature:  signature,
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
		X: (*big.Int)(&resp.EncryptionPubKey[0]),
		Y: (*big.Int)(&resp.EncryptionPubKey[1]),
	}

	// Create process in the contracts
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
		return nil, nil, fmt.Errorf("failed to create process in contracts: %v", err)
	}

	// Wait for the process creation transaction to be mined
	if err = contracts.WaitTx(*txHash, time.Second*15); err != nil {
		return nil, nil, fmt.Errorf("failed to wait for process creation tx: %v", err)
	}

	// Wait for the process to be registered in the sequencer
	processCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for processCreated := false; !processCreated; {
		select {
		case <-time.After(time.Second * 5):
			_, status, err := cli.Request(http.MethodGet, nil, nil, api.EndpointWithParam(api.ProcessEndpoint, api.ProcessURLParam, pid.String()))
			if err == nil && status == http.StatusOK {
				processCreated = true
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
	secret := util.RandomBytes(16)
	k, err := elgamal.RandK()
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate random k: %v", err)
	}

	// Generate random ballot fields
	randFields := ballotprooftest.GenBallotFieldsForTest(
		int(bm.MaxCount),
		int(bm.MaxValue.MathBigInt().Int64()),
		int(bm.MinValue.MathBigInt().Int64()),
		bm.ForceUniqueness)

	// Cast fields to types.BigInt
	fields := []*types.BigInt{}
	for _, f := range randFields {
		fields = append(fields, (*types.BigInt)(f))
	}

	// Compose wasm inputs
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
	circomProof, _, err := circuits.Circom2GnarkProof(rawProof, pubInputs)
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
		ProcessID:        wasmResult.ProccessID,
		Address:          wasmInputs.Address,
		Commitment:       wasmResult.Commitment,
		Nullifier:        wasmResult.Nullifier,
		Ballot:           wasmResult.Ballot,
		BallotProof:      circomProof,
		BallotInputsHash: wasmResult.BallotInputsHash,
		Signature:        signature.Bytes(),
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
	body, status, err := cli.Request(http.MethodPost, vote, nil, api.VotesEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to cast vote: %v", err)
	} else if status != http.StatusOK {
		return nil, fmt.Errorf("failed to cast vote, status code: %d", status)
	}

	// Parse the response body to get the vote ID
	var voteResponse api.VoteResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&voteResponse); err != nil {
		return nil, fmt.Errorf("failed to decode vote response: %v", err)
	}

	return voteResponse.VoteID, nil
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
