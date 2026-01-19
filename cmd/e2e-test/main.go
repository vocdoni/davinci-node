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
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/sequencer"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
	"github.com/vocdoni/davinci-node/util/circomgnark"
	"github.com/vocdoni/davinci-node/web3"
	"github.com/vocdoni/davinci-node/web3/rpc/chainlist"
)

const (
	defaultNetwork     = "sepolia"
	defaultCAPI        = "https://ethereum-sepolia-beacon-api.publicnode.com"
	localSequencerHost = "0.0.0.0"
	localSequencerPort = 8080
	defaultCensus3URL  = "https://c3-dev.davinci.vote"
)

var (
	localSequencerEndpoint = fmt.Sprintf("http://%s:%d", localSequencerHost, localSequencerPort)

	userWeight = uint64(testutil.Weight)
	ballotMode = testutil.BallotModeInternal()
)

func main() {
	// define cli flags
	var (
		privKey                          = flag.String("privkey", "", "private key to use for the Ethereum account")
		web3rpcs                         = flag.StringSlice("web3rpcs", nil, "web3 rpc http endpoints")
		consensusAPI                     = flag.String("consensusAPI", defaultCAPI, "web3 consensus API http endpoint")
		organizationRegistryAddress      = flag.String("organizationRegistryAddress", "", "organization registry smart contract address")
		processRegistryAddress           = flag.String("processRegistryAddress", "", "process registry smart contract address")
		stateTransitionZKVerifierAddress = flag.String("stateTransitionZKVerifierAddress", "", "state transition zk verifier smart contract address")
		resultsZKVerifierAddress         = flag.String("resultsZKVerifierAddress", "", " results zk verifier smart contract address")
		testTimeout                      = flag.Duration("timeout", 20*time.Minute, "timeout for the test")
		sequencerEndpoints               = flag.StringSlice("sequencerEndpoint", []string{}, "sequencer endpoint(s)")
		census3URL                       = flag.String("census3URL", defaultCensus3URL, "census3 endpoint")
		votersCount                      = flag.Int("votersCount", 10, "number of voters that will cast a vote (half of them will rewrite it)")
		voteSleepTime                    = flag.Duration("voteSleepTime", 10*time.Second, "time to sleep between votes")
		web3Network                      = flag.StringP("web3.network", "n", defaultNetwork, fmt.Sprintf("network to use %v", npbindings.AvailableNetworksByName))
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

	log.Infow("using web3 configuration",
		"network", *web3Network,
		"organizationRegistryAddress", *organizationRegistryAddress,
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

	if organizationRegistryAddress == nil || *organizationRegistryAddress == "" {
		*organizationRegistryAddress, err = contracts.OrganizationRegistryAddress()
		if err != nil {
			log.Fatal(err)
		}
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
			log.Warnw("failed to ping sequencer", "status", status, "error", err)
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
	{
		votes, err := createVotes(signers, processID, encryptionKey)
		if err != nil {
			log.Errorw(err, "failed to create votes")
			return
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

	}

	log.Info("first batch of votes registered in smart contract, will now overwrite half of them")
	overwriters := signers[:len(signers)/2]
	overwrittenVotesCount := 0
	for i, sequencer := range sequencers {
		log.Infof("now overwriting votes, using sequencer %d: %s", i, sequencer)
		votes, err := createVotes(overwriters, processID, encryptionKey)
		if err != nil {
			log.Errorw(err, "failed to create vote overwrites")
			return
		}
		if err := sendVotesToSequencer(testCtx, sequencer, *voteSleepTime, votes); err != nil {
			log.Errorw(err, "failed to send vote overwrites")
			return
		}
		overwrittenVotesCount += len(overwriters)

		// Wait for the votes to be registered in the smart contract
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
	log.Infow("on-chain results received", "processID", processID.String(), "results", results)
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
	sequencer.AggregatorTickerInterval = time.Second * 2
	sequencer.NewProcessMonitorInterval = time.Second * 5
	// Start census downloader
	s.censusDownloader = service.NewCensusDownloader(contracts, s.storage, service.CensusDownloaderConfig{
		CleanUpInterval: time.Second * 5,
		Attempts:        5,
		Expiration:      time.Minute * 30,
		Cooldown:        time.Second * 10,
	})
	if err := s.censusDownloader.Start(ctx); err != nil {
		return fmt.Errorf("failed to start census downloader: %w", err)
	}
	// Start StateSync
	stateSync := service.NewStateSync(contracts, s.storage)
	if err := stateSync.Start(ctx); err != nil {
		return fmt.Errorf("failed to start state sync: %v", err)
	}
	// Monitor new processes from the contracts
	s.processMonitor = service.NewProcessMonitor(contracts, s.storage, s.censusDownloader, stateSync, time.Second*2)
	if err := s.processMonitor.Start(ctx); err != nil {
		return fmt.Errorf("failed to start process monitor: %v", err)
	}
	// Start sequencer service
	s.sequencer = service.NewSequencer(s.storage, contracts, time.Second*30, nil)
	if err := s.sequencer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sequencer: %v", err)
	}
	// Start API service
	_, ok := npbindings.AvailableNetworksByName[network]
	if !ok {
		return fmt.Errorf("invalid network configuration for %s", network)
	}
	c := npbindings.GetAllContractAddresses(network)
	web3Conf := config.DavinciWeb3Config{
		ProcessRegistrySmartContract:      c[npbindings.ProcessRegistryContract],
		OrganizationRegistrySmartContract: c[npbindings.OrganizationRegistryContract],
		ResultsZKVerifier:                 c[npbindings.ResultsVerifierGroth16Contract],
		StateTransitionZKVerifier:         c[npbindings.StateTransitionVerifierGroth16Contract],
	}
	s.api = service.NewAPI(s.storage, localSequencerHost, localSequencerPort, network, web3Conf, false)
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
	if err := contracts.WaitTxByHash(txHash, time.Second*30); err != nil {
		return common.Address{}, err
	}

	return orgAddr, nil
}

func sendVotesToSequencer(ctx context.Context, seqEndpoint string, sleepTime time.Duration, votes []api.Vote) error {
	// Create a API client
	cli, err := client.New(seqEndpoint)
	if err != nil {
		log.Fatal(err)
	}

	// Wait for the sequencer to be ready, make ping request until it responds
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
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
			log.Warnw("failed to ping sequencer", "status", status, "error", err)
			time.Sleep(10 * time.Second)
		}
	}
	log.Infow("connected to sequencer", "endpoint", seqEndpoint)

	// Generate votes for each participant and send them to the sequencer
	for i, vote := range votes {
		// Send the vote to the sequencer
		voteID, err := sendVote(cli, vote)
		if err != nil {
			log.Errorf("failed to send this vote: %+v", vote)
			return fmt.Errorf("failed to send vote: %w", err)
		}
		log.Infow("vote sent",
			"voteID", voteID.String(),
			"currentVote", i+1,
			"totalVotes", len(votes))

		// Wait the sleepTime before sending the next vote
		time.Sleep(sleepTime)
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
	ballotMode *types.BallotMode,
	maxVoters *types.BigInt,
) (types.ProcessID, *types.EncryptionKey, error) {
	// Create test process request

	processId, err := contracts.NextProcessID(contracts.AccountAddress())
	if err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to get next process ID: %v", err)
	}

	// Sign the process creation request
	signature, err := contracts.SignMessage(fmt.Appendf(nil, types.NewProcessMessageToSign, processId.String()))
	if err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to sign process creation request: %v", err)
	}

	// Make the request to create the process
	process := &types.ProcessSetup{
		ProcessID:  processId,
		BallotMode: ballotMode,
		Signature:  signature,
		Census: &types.Census{
			CensusRoot:   censusRoot,
			CensusURI:    censusURI,
			CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
		},
	}
	body, code, err := cli.Request(http.MethodPost, process, nil, api.ProcessesEndpoint)
	if err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to create process: %v", err)
	} else if code != http.StatusOK {
		return types.ProcessID{}, nil, fmt.Errorf("failed to create process, status code: %d", code)
	}

	// Decode process response
	var resp types.ProcessSetupResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&resp); err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to decode process response: %v", err)
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
	for processReady := false; !processReady; {
		select {
		case <-time.After(time.Second * 5):
			pBytes, status, err := cli.Request(http.MethodGet, nil, nil, api.EndpointWithParam(api.ProcessEndpoint, api.ProcessURLParam, processID.String()))
			if err == nil && status == http.StatusOK {
				proc := &api.ProcessResponse{}
				if err := json.Unmarshal(pBytes, proc); err != nil {
					return types.ProcessID{}, nil, fmt.Errorf("failed to unmarshal process response: %v", err)
				}
				processReady = proc.IsAcceptingVotes
			}
		case <-processCtx.Done():
			return types.ProcessID{}, nil, fmt.Errorf("process creation timeout: %v", processCtx.Err())
		}
	}
	time.Sleep(5 * time.Second) // wait a bit more to ensure everything is set up
	return processID, encryptionKeys, nil
}

func createVotes(signers []*ethereum.Signer, processID types.ProcessID, encryptionKey *types.EncryptionKey) ([]api.Vote, error) {
	votes := make([]api.Vote, 0, len(signers))
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
		CensusProof: types.CensusProof{
			Weight: new(types.BigInt).SetInt(testutil.Weight),
		},
	}, nil
}

func sendVote(cli *client.HTTPclient, vote api.Vote) (types.HexBytes, error) {
	// Make the request to cast the vote
	body, status, err := cli.Request(http.MethodPost, vote, nil, api.VotesEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to cast vote: %w", err)
	} else if status != http.StatusOK {
		return nil, fmt.Errorf("failed to cast vote (status code %d): %s", status, body)
	}
	return vote.VoteID, nil
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
