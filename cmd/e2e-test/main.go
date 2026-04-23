package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	flag "github.com/spf13/pflag"
	"github.com/vocdoni/arbo/memdb"
	npbindings "github.com/vocdoni/davinci-contracts/golang-types"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/api/client"
	censustest "github.com/vocdoni/davinci-node/census/test"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	ballotprooftest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/metadata"
	"github.com/vocdoni/davinci-node/sequencer"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/spec"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
	"github.com/vocdoni/davinci-node/util/circomgnark"
	"github.com/vocdoni/davinci-node/web3"
	"github.com/vocdoni/davinci-node/web3/rpc/chainlist"
	"golang.org/x/sync/errgroup"
)

const (
	defaultNetwork     = "sep"
	defaultCAPI        = "https://ethereum-sepolia-beacon-api.publicnode.com"
	localSequencerHost = "0.0.0.0"
	localSequencerPort = 8080
	defaultCensus3URL  = "https://c3-dev.davinci.vote"
)

var (
	localSequencerEndpoint = fmt.Sprintf("http://%s:%d", localSequencerHost, localSequencerPort)

	userWeight = uint64(testutil.Weight)
	ballotMode = testutil.BallotMode()
)

type VoteWithValues struct {
	api.Vote
	FieldValues []*types.BigInt
}

func main() {
	// define cli flags
	var (
		privKey                          = flag.String("privkey", "", "private key to use for the Ethereum account")
		web3rpcs                         = flag.StringSlice("web3rpcs", nil, "web3 rpc http endpoints")
		consensusAPI                     = flag.String("consensusAPI", defaultCAPI, "web3 consensus API http endpoint")
		processRegistryAddress           = flag.String("processRegistryAddress", "", "process registry smart contract address")
		stateTransitionZKVerifierAddress = flag.String("stateTransitionZKVerifierAddress", "", "state transition zk verifier smart contract address")
		resultsZKVerifierAddress         = flag.String("resultsZKVerifierAddress", "", " results zk verifier smart contract address")
		testTimeout                      = flag.Duration("timeout", 20*time.Minute, "timeout for the test")
		sequencerEndpoints               = flag.StringSlice("sequencerEndpoint", []string{}, "sequencer endpoint(s)")
		census3URL                       = flag.String("census3URL", defaultCensus3URL, "census3 endpoint")
		votersCount                      = flag.Int("votersCount", 10, "number of voters that will cast a vote (half of them will rewrite it)")
		voteSleepTime                    = flag.Duration("voteSleepTime", 10*time.Second, "time to sleep between votes")
		web3Network                      = flag.StringP("web3.network", "n", defaultNetwork, fmt.Sprintf("network to use %v", npbindings.AvailableNetworksByName))
		parallel                         = flag.Bool("parallel", false, "cast votes to different sequencers at the same time")
		debugLevel                       = flag.String("debug", "debug", "debug level")
	)
	flag.Parse()
	log.Init(*debugLevel, "stdout", nil)

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

	log.Infow("using web3 configuration",
		"network", *web3Network,
		"processRegistryAddress", *processRegistryAddress,
		"stateTransitionZKVerifierAddress", *stateTransitionZKVerifierAddress,
		"resultsZKVerifierAddress", *resultsZKVerifierAddress,
		"web3rpcs", *web3rpcs,
		"voteSleepTime", *voteSleepTime,
	)

	// Intance contracts with the provided web3rpcs
	contracts, err := web3.New(*web3rpcs, *consensusAPI, 1.0)
	if err != nil {
		log.Fatal(err)
	}

	if processRegistryAddress == nil || *processRegistryAddress == "" {
		*processRegistryAddress, err = contracts.ProcessRegistryAddress()
		if err != nil {
			log.Fatal(err)
		}
	}

	if stateTransitionZKVerifierAddress == nil || *stateTransitionZKVerifierAddress == "" {
		*stateTransitionZKVerifierAddress, err = contracts.StateTransitionVerifierAddress()
		if err != nil {
			log.Fatal(err)
		}
	}

	if resultsZKVerifierAddress == nil || *resultsZKVerifierAddress == "" {
		*resultsZKVerifierAddress, err = contracts.ResultsVerifierAddress()
		if err != nil {
			log.Fatal(err)
		}
	}

	// Load contracts from the default config
	if err = contracts.LoadContracts(nil); err != nil {
		log.Fatal(err)
	}
	// Add the web3rpcs to the contracts
	for i := range *web3rpcs {
		if err := contracts.AddWeb3Endpoint((*web3rpcs)[i]); err != nil {
			log.Warnw("failed to add endpoint", "rpc", (*web3rpcs)[i], "error", err)
		}
	}
	// Set the private key for the account
	if err := contracts.SetAccountPrivateKey(util.TrimHex(*privKey)); err != nil {
		log.Fatal(err)
	}
	log.Infow("contracts initialized", "chainId", contracts.ChainID)

	sequencers := *sequencerEndpoints

	// If no sequencer endpoint is provided, start a local one
	if len(sequencers) == 0 {
		log.Infow("no remote sequencer endpoint provided, starting a local one...")
		// Start a local sequencer
		service := new(localService)
		if err := service.Start(testCtx, contracts, *web3Network); err != nil {
			log.Fatal(err)
		}
		defer service.Stop()
		log.Infow("local sequencer started", "endpoint", localSequencerEndpoint)
		sequencers = append(sequencers, localSequencerEndpoint)
	}
	// Create a API client
	cli, err := client.New(sequencers[0])
	if err != nil {
		log.Fatal(err)
	}
	// Wait for the sequencer to be ready, make ping request until it responds
	pingCtx, cancel := context.WithTimeout(testCtx, 2*time.Minute)
	defer cancel()
	for {
		_, status, err := cli.Request(http.MethodGet, nil, nil, api.PingEndpoint)
		if err == nil && status == http.StatusOK {
			break
		}
		log.Warnw("failed to ping sequencer", "status", status, "error", err)

		select {
		case <-pingCtx.Done():
			log.Fatal("ping timeout")
		case <-time.After(10 * time.Second):
		}
	}
	log.Info("connected to sequencer")

	// Create a new census with numBallot participants
	censusRoot, censusURI, signers, err := createCensus(testCtx, *votersCount, userWeight, *census3URL)
	if err != nil {
		log.Errorw(err, "failed to create census")
		return
	}
	log.Infow("census created",
		"root", censusRoot.String(),
		"participants", len(signers))

	// Create a new process with mocked ballot mode
	processID, encryptionKey, err := createProcess(testCtx, contracts, cli, censusRoot, censusURI, ballotMode, new(types.BigInt).SetInt(*votersCount))
	if err != nil {
		log.Errorw(err, "failed to create process")
		return
	}
	log.Infow("process created", "processID", processID.String())

	// Generate votes for each participant and send them to the sequencer
	expectedResultsByAddress := make(map[common.Address][]*types.BigInt, len(signers))
	{
		votes, err := createVotes(signers, processID, encryptionKey)
		if err != nil {
			log.Errorw(err, "failed to create votes")
			return
		}
		for _, vote := range votes {
			expectedResultsByAddress[common.BytesToAddress(vote.Address)] = vote.FieldValues
		}

		if err := sendVotesToSequencer(testCtx, sequencers[0], *voteSleepTime, votes); err != nil {
			log.Errorw(err, "failed to send votes")
			return
		}

		// Wait for the votes to be registered in the smart contract
		log.Info("all votes sent, waiting for votes to be registered in smart contract...")

		if err := waitUntilSmartContractCounts(testCtx, contracts, processID, *votersCount, 0); err != nil {
			log.Errorw(err, "failed to wait for votes to be registered in smart contract")
			return
		}
		for _, v := range votes {
			// Check if the participant has already voted
			if err := waitForAddressHasAlreadyVoted(testCtx, cli, v.ProcessID, v.Address); err != nil {
				log.Errorw(err, "failed to ensure that the vote is in the state")
			}
		}

	}

	log.Info("first batch of votes registered in smart contract, will now overwrite half of them")
	overwriters := signers[:len(signers)/2]
	overwrittenVotesCount := 0
	group, groupCtx := errgroup.WithContext(testCtx)
	for i, sequencer := range sequencers {
		votes, err := createVotes(overwriters, processID, encryptionKey)
		if err != nil {
			log.Errorw(err, "failed to create vote overwrites")
			return
		}
		for _, vote := range votes {
			expectedResultsByAddress[common.BytesToAddress(vote.Address)] = vote.FieldValues
		}
		overwrittenVotesCount += len(overwriters)
		log.Infof("now overwriting votes, using sequencer %d (%s)", i, sequencer)
		if *parallel {
			sequencer, votes := sequencer, votes
			group.Go(func() error { return sendVotesToSequencer(groupCtx, sequencer, *voteSleepTime, votes) })
		} else {
			if err := sendVotesToSequencer(testCtx, sequencer, *voteSleepTime, votes); err != nil {
				log.Errorw(err, "failed to send vote overwrites")
				return
			}

			log.Infof("overwrite votes sent to sequencer %d (%s), waiting for votes to be registered in smart contract...", i, sequencer)
			if err := waitUntilSmartContractCounts(testCtx, contracts, processID, *votersCount, overwrittenVotesCount); err != nil {
				log.Errorw(err, "failed to wait for votes to be registered in smart contract")
				return
			}
		}
	}
	if err := group.Wait(); err != nil {
		log.Errorw(err, "failed to send vote overwrites")
		return
	}
	if *parallel {
		log.Info("all overwrite votes sent, waiting for votes to be registered in smart contract...")
		if err := waitUntilSmartContractCounts(testCtx, contracts, processID, *votersCount, overwrittenVotesCount); err != nil {
			log.Errorw(err, "failed to wait for votes to be registered in smart contract")
			return
		}
	}

	log.Info("finishing the process in the smart contract...")
	// finish the process in the smart contract
	if err := finishProcessOnChain(contracts, processID); err != nil {
		log.Errorw(err, "failed to finish process in smart contract")
		return
	}
	log.Infow("process finished in smart contract", "processID", processID.String())
	// Wait for the process to be finished in the sequencer
	resultsCtx, cancel := context.WithTimeout(testCtx, 2*time.Minute)
	defer cancel()
	results, err := waitForOnChainResults(resultsCtx, contracts, processID)
	if err != nil {
		log.Errorw(err, "failed to wait for on-chain results")
		return
	}
	expectedResults := calculateExpectedResults(expectedResultsByAddress)
	if err := compareResults(expectedResults, results); err != nil {
		log.Infow("final results mismatch details", "processID", processID.String(), "expected", expectedResults, "actual", results)
		log.Errorw(err, "final results mismatch")
		return
	}
	log.Infow("on-chain results received and verified against expected tally",
		"processID", processID.String(), "results", results)
}

type localService struct {
	sequencer        *service.SequencerService
	censusDownloader *service.CensusDownloader
	processMonitor   *service.ProcessMonitor
	storage          *storage.Storage
	api              *service.APIService
}

func (s *localService) Start(ctx context.Context, contracts *web3.Contracts, network string) error {
	// Create storage with a in-memory database
	s.storage = storage.New(memdb.New())
	apiRuntime, err := web3.NewNetworkRuntime(contracts, nil)
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}
	runtimeRouter, err := web3.NewRuntimeRouter(apiRuntime)
	if err != nil {
		return fmt.Errorf("failed to create runtime router: %w", err)
	}
	sequencer.AggregatorTickerInterval = time.Second * 2
	sequencer.NewProcessMonitorInterval = time.Second * 5
	// Start census downloader
	s.censusDownloader = service.NewCensusDownloader(runtimeRouter, s.storage, service.DefaultCensusDownloaderConfig)
	if err := s.censusDownloader.Start(ctx); err != nil {
		return fmt.Errorf("failed to start census downloader: %w", err)
	}
	// Start StateSync
	stateSync := service.NewStateSync(runtimeRouter, s.storage)
	if err := stateSync.Start(ctx); err != nil {
		return fmt.Errorf("failed to start state sync: %v", err)
	}
	// Monitor new processes from the contracts
	s.processMonitor = service.NewProcessMonitor(contracts, apiRuntime.ProcessIDVersion, s.storage, s.censusDownloader, stateSync, time.Second*2)
	if err := s.processMonitor.Start(ctx); err != nil {
		return fmt.Errorf("failed to start process monitor: %v", err)
	}
	// Start sequencer service
	s.sequencer = service.NewSequencer(s.storage, runtimeRouter, time.Second*30, nil)
	if err := s.sequencer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sequencer: %v", err)
	}
	// Start API service
	s.api = service.NewAPI(s.storage, localSequencerHost, localSequencerPort, runtimeRouter, metadata.PinataMetadataProviderConfig{}, false)
	if err := s.api.Start(ctx); err != nil {
		return fmt.Errorf("failed to start API: %v", err)
	}
	return nil
}

func (s *localService) Stop() {
	if s.sequencer != nil {
		s.sequencer.Stop()
	}
	if s.censusDownloader != nil {
		s.censusDownloader.Stop()
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

func sendVotesToSequencer(ctx context.Context, seqEndpoint string, sleepTime time.Duration, votes []VoteWithValues) error {
	// Create a API client
	cli, err := client.New(seqEndpoint)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Wait for the sequencer to be ready, make ping request until it responds
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		_, status, err := cli.Request(http.MethodGet, nil, nil, api.PingEndpoint)
		if err == nil && status == http.StatusOK {
			break
		}
		log.Warnw("failed to ping sequencer", "status", status, "error", err)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pingCtx.Done():
			return fmt.Errorf("ping timeout: %w", pingCtx.Err())
		case <-time.After(10 * time.Second):
		}
	}
	log.Infow("connected to sequencer", "endpoint", seqEndpoint)

	// Generate votes for each participant and send them to the sequencer
	for i, vote := range votes {
		// Send the vote to the sequencer
		voteID, err := sendVote(cli, vote.Vote)
		if err != nil {
			log.Errorf("failed to send this vote: %+v", vote.Vote)
			return fmt.Errorf("failed to send vote: %w", err)
		}
		log.Infow("vote sent",
			"processID", vote.ProcessID.String(),
			"address", vote.Address.Hex(),
			"voteID", voteID.String(),
			"currentVote", i+1,
			"totalVotes", len(votes))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepTime):
		}
	}
	return nil
}

func createCensus(ctx context.Context, size int, weight uint64, c3URL string) (types.HexBytes, string, []*ethereum.Signer, error) {
	// Generate random participants
	signers := []*ethereum.Signer{}
	votes := []state.Vote{}
	for range size {
		signer, err := ethereum.NewSigner()
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to generate signer: %w", err)
		}
		signers = append(signers, signer)
		votes = append(votes, state.Vote{
			Address: signer.Address().Big(),
			Weight:  new(big.Int).SetUint64(weight),
		})
	}
	censusRoot, censusURI, err := censustest.NewCensus3MerkleTreeForTest(ctx, types.CensusOriginMerkleTreeOffchainStaticV1, votes, c3URL)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to serve census merkle tree: %w", err)
	}
	return censusRoot, censusURI, signers, nil
}

func createProcess(
	ctx context.Context,
	contracts *web3.Contracts,
	cli *client.HTTPclient,
	censusRoot types.HexBytes,
	censusURI string,
	ballotMode spec.BallotMode,
	maxVoters *types.BigInt,
) (types.ProcessID, *types.EncryptionKey, error) {
	// Make the request to get the encryption keys
	body, code, err := cli.Request(http.MethodPost, nil, nil, api.NewEncryptionKeysEndpoint)
	if err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to create process: %v", err)
	} else if code != http.StatusOK {
		return types.ProcessID{}, nil, fmt.Errorf("failed to create process, status code: %d", code)
	}

	// Decode process response
	var resp types.ProcessEncryptionKeysResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&resp); err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to decode process response: %v", err)
	}
	encryptionKeys := &types.EncryptionKey{
		X: resp.EncryptionPubKey[0],
		Y: resp.EncryptionPubKey[1],
	}

	newProcess := &types.Process{
		Status:         0,
		OrganizationID: contracts.AccountAddress(),
		EncryptionKey:  encryptionKeys,
		StartTime:      time.Now().Add(1 * time.Minute),
		Duration:       time.Hour,
		MetadataURI:    "https://example.com/metadata",
		BallotMode:     ballotMode,
		MaxVoters:      maxVoters,
		Census: &types.Census{
			CensusRoot:   censusRoot,
			CensusURI:    censusURI,
			CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
		},
	}
	// Create process in the contracts
	processID, txHash, err := contracts.CreateProcess(newProcess)
	if err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to create process in contracts: %v", err)
	}

	// Wait for the process creation transaction to be mined
	if err = contracts.WaitTxByHash(*txHash, time.Minute*2); err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to wait for process creation tx: %v", err)
	}

	// Wait for the process to be registered in the sequencer
	processCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		pBytes, status, err := cli.Request(http.MethodGet, nil, nil, api.EndpointWithParam(api.ProcessEndpoint, api.ProcessURLParam, processID.String()))
		if err == nil && status == http.StatusOK {
			proc := &api.ProcessResponse{}
			if err := json.Unmarshal(pBytes, proc); err != nil {
				return types.ProcessID{}, nil, fmt.Errorf("failed to unmarshal process response: %v", err)
			}
			if proc.IsAcceptingVotes {
				break
			}
		}
		select {
		case <-processCtx.Done():
			return types.ProcessID{}, nil, fmt.Errorf("process creation timeout: %v", processCtx.Err())
		case <-time.After(time.Second * 5):
		}
	}
	time.Sleep(5 * time.Second) // wait a bit more to ensure everything is set up
	return processID, encryptionKeys, nil
}

func createVotes(signers []*ethereum.Signer, processID types.ProcessID, encryptionKey *types.EncryptionKey) ([]VoteWithValues, error) {
	votes := make([]VoteWithValues, 0, len(signers))
	for _, signer := range signers {
		vote, err := createVote(signer, processID, encryptionKey, ballotMode)
		if err != nil {
			return nil, fmt.Errorf("failed to create vote: %w", err)
		}
		votes = append(votes, vote)
	}
	return votes, nil
}

func createVote(
	privKey *ethereum.Signer,
	processID types.ProcessID,
	encKey *types.EncryptionKey,
	bm spec.BallotMode,
) (VoteWithValues, error) {
	// Emulate user inputs
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	k, err := specutil.RandomK()
	if err != nil {
		return VoteWithValues{}, fmt.Errorf("failed to generate random k: %v", err)
	}

	// Generate random ballot fields
	randFields := ballotprooftest.GenBallotFieldsForTest(
		int(bm.NumFields),
		int(bm.MaxValue),
		int(bm.MinValue),
		bm.UniqueValues)

	// Cast fields to types.BigInt
	fields := []*types.BigInt{}
	for _, f := range randFields {
		fields = append(fields, (*types.BigInt)(f))
	}

	// Compose wasm inputs
	wasmInputs := &ballotproof.BallotProofInputs{
		Address:   address.Bytes(),
		ProcessID: processID,
		EncryptionKey: []*types.BigInt{
			(*types.BigInt)(encKey.X),
			(*types.BigInt)(encKey.Y),
		},
		K:           (*types.BigInt)(k),
		BallotMode:  bm,
		Weight:      new(types.BigInt).SetInt(testutil.Weight),
		FieldValues: fields,
	}

	// Generate the inputs for the ballot proof circuit
	wasmResult, err := ballotproof.GenerateBallotProofInputs(wasmInputs)
	if err != nil {
		return VoteWithValues{}, fmt.Errorf("failed to generate ballot proof inputs: %v", err)
	}

	// Encode the inputs to json
	encodedCircomInputs, err := json.Marshal(wasmResult.CircomInputs)
	if err != nil {
		return VoteWithValues{}, fmt.Errorf("failed to encode circom inputs: %v", err)
	}

	// Generate the proof using the circom circuit
	rawProof, pubInputs, err := ballotprooftest.CompileAndGenerateProofForTest(encodedCircomInputs)
	if err != nil {
		return VoteWithValues{}, fmt.Errorf("failed to generate proof: %v", err)
	}

	// Convert the proof to gnark format
	circomProof, _, err := circomgnark.UnmarshalCircom(rawProof, pubInputs)
	if err != nil {
		return VoteWithValues{}, fmt.Errorf("failed to convert proof to gnark format: %v", err)
	}

	// Sign the hash of the circuit inputs
	signature, err := ballotprooftest.SignECDSAForTest(privKey, wasmResult.VoteID)
	if err != nil {
		return VoteWithValues{}, fmt.Errorf("failed to sign vote: %v", err)
	}

	// Return the vote ready to be sent to the sequencer
	return VoteWithValues{
		Vote: api.Vote{
			ProcessID:        wasmResult.ProcessID,
			Address:          wasmInputs.Address,
			Ballot:           wasmResult.Ballot,
			BallotProof:      circomProof,
			BallotInputsHash: wasmResult.BallotInputsHash,
			Signature:        signature.Bytes(),
			VoteID:           wasmResult.VoteID,
			CensusProof: types.CensusProof{
				Weight: new(types.BigInt).SetInt(testutil.Weight),
			},
		},
		FieldValues: fields,
	}, nil
}

func calculateExpectedResults(fieldValuesByAddress map[common.Address][]*types.BigInt) []*types.BigInt {
	var expectedResults []*types.BigInt
	for _, fieldValues := range fieldValuesByAddress {
		if expectedResults == nil {
			expectedResults = make([]*types.BigInt, len(fieldValues))
			for i := range expectedResults {
				expectedResults[i] = types.NewInt(0)
			}
		}
		for i, fieldValue := range fieldValues {
			expectedResults[i] = expectedResults[i].Add(expectedResults[i], fieldValue)
		}
	}
	if expectedResults == nil {
		return []*types.BigInt{}
	}
	return expectedResults
}

func compareResults(expected, actual []*types.BigInt) error {
	if len(expected) != len(actual) {
		return fmt.Errorf("unexpected results length: expected %d, got %d", len(expected), len(actual))
	}
	for i := range expected {
		if expected[i].Cmp(actual[i]) != 0 {
			return fmt.Errorf("result mismatch at index %d: expected %s, got %s", i, expected[i].String(), actual[i].String())
		}
	}
	return nil
}

func sendVote(cli *client.HTTPclient, vote api.Vote) (types.VoteID, error) {
	// Make the request to cast the vote
	body, status, err := cli.Request(http.MethodPost, vote, nil, api.VotesEndpoint)
	if err != nil {
		return 0, fmt.Errorf("failed to cast vote: %w", err)
	} else if status != http.StatusOK {
		return 0, fmt.Errorf("failed to cast vote (status code %d): %s", status, body)
	}
	return vote.VoteID, nil
}

func hasAlreadyVoted(cli *client.HTTPclient, pid types.ProcessID, address common.Address) (bool, error) {
	// get participant from the sequencer
	voteByAddressProcessEndpoint := api.EndpointWithParam(api.VoteByAddressEndpoint, api.ProcessURLParam, pid.String())
	voteByAddressEndpoint := api.EndpointWithParam(voteByAddressProcessEndpoint, api.AddressURLParam, address.Hex())
	voteByAddressBody, statusCode, err := cli.Request("GET", nil, nil, voteByAddressEndpoint)
	if err != nil {
		return false, fmt.Errorf("failed to request participant: %w", err)
	}
	if statusCode == http.StatusNotFound {
		return false, nil
	}
	if statusCode != 200 {
		return false, fmt.Errorf("unexpected status code: %d: %s", statusCode, string(voteByAddressBody))
	}
	var voteByAddressResponse *elgamal.Ballot
	err = json.NewDecoder(bytes.NewReader(voteByAddressBody)).Decode(&voteByAddressResponse)
	if err != nil {
		return false, fmt.Errorf("failed to decode already voted response: %w", err)
	}
	return voteByAddressResponse != nil, nil
}

func waitForAddressHasAlreadyVoted(ctx context.Context, cli *client.HTTPclient, pid types.ProcessID, address types.HexBytes) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ok, err := hasAlreadyVoted(cli, pid, common.BytesToAddress(address))
			if err != nil {
				return err
			}
			if ok {
				return nil
			}
		}
	}
}

func waitUntilSmartContractCounts(
	ctx context.Context,
	contracts *web3.Contracts,
	processID types.ProcessID,
	votersCount, overwrittenVotesCount int,
) error {
	ticker := time.NewTicker(time.Second * 30)
	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				return nil
			}
			return fmt.Errorf("process creation timeout: %v", ctx.Err())
		case <-ticker.C:
			process, err := contracts.Process(processID)
			if err != nil {
				return fmt.Errorf("failed to get process: %v", err)
			}
			if process == nil {
				return fmt.Errorf("process not found")
			}
			// Get the voters count from the process
			if process.VotersCount != nil && process.OverwrittenVotesCount != nil {
				log.Debugw("polled smart contract counters", "processID", processID.String(),
					"targetVoters", votersCount, "targetOverwritten", overwrittenVotesCount,
					"votersCount", process.VotersCount, "overwrittenVotesCount", process.OverwrittenVotesCount)
				if process.VotersCount.MathBigInt().Int64() >= int64(votersCount) &&
					process.OverwrittenVotesCount.MathBigInt().Int64() >= int64(overwrittenVotesCount) {
					return nil
				}
			}
		}
	}
}

func finishProcessOnChain(contracts *web3.Contracts, processID types.ProcessID) error {
	finishTx, err := contracts.SetProcessStatus(processID, types.ProcessStatusEnded)
	if err != nil {
		return fmt.Errorf("failed to finish process: %v", err)
	}
	if err := contracts.WaitTxByHash(*finishTx, time.Second*30); err != nil {
		return fmt.Errorf("failed to wait for process finish tx: %v", err)
	}
	return nil
}

func waitForOnChainResults(ctx context.Context, contracts *web3.Contracts, processID types.ProcessID) ([]*types.BigInt, error) {
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			process, err := contracts.Process(processID)
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
