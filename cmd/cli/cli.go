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
)

type CLIServices struct {
	sequencer        *service.SequencerService
	censusDownloader *service.CensusDownloader
	processMonitor   *service.ProcessMonitor
	storage          *storage.Storage
	api              *service.APIService

	cli       *client.HTTPclient
	contracts *web3.Contracts
	addresses *web3.Addresses
	network   string

	ctx    context.Context
	cancel context.CancelFunc
}

func NewCLIServices(ctx context.Context) *CLIServices {
	ctx, cancel := context.WithCancel(ctx)
	return &CLIServices{
		ctx:    ctx,
		cancel: cancel,
	}
}

func (s *CLIServices) Init(
	network string,
	rpcs []string,
	consensusAPI string,
	organizationRegistryAddress string,
	processRegistryAddress string,
	stateTransitionZKVerifierAddress string,
	resultsZKVerifierAddress string,
	privKey string,
) error {
	if err := s.initContracts(
		network,
		rpcs,
		consensusAPI,
		organizationRegistryAddress,
		processRegistryAddress,
		stateTransitionZKVerifierAddress,
		resultsZKVerifierAddress,
		privKey,
	); err != nil {
		return err
	}

	return s.initSequencerCLI()
}

func (s *CLIServices) Start(ctx context.Context, contracts *web3.Contracts, network string) error {
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

func (s *CLIServices) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	// Stop services
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

func (s *CLIServices) initContracts(
	network string,
	rpcs []string,
	consensusAPI string,
	organizationRegistryAddress string,
	processRegistryAddress string,
	stateTransitionZKVerifierAddress string,
	resultsZKVerifierAddress string,
	privKey string,
) error {
	log.Infow("using web3 configuration",
		"network", network,
		"organizationRegistryAddress", organizationRegistryAddress,
		"processRegistryAddress", processRegistryAddress,
		"stateTransitionZKVerifierAddress", stateTransitionZKVerifierAddress,
		"resultsZKVerifierAddress", resultsZKVerifierAddress,
		"web3rpcs", rpcs,
		"consensusAPI", consensusAPI,
		"voteSleepTime", *voteSleepTime,
	)

	// Instance contracts with the provided web3rpcs
	var err error
	s.contracts, err = web3.New(rpcs, consensusAPI, 1.0)
	if err != nil {
		return fmt.Errorf("error initializing web3 contracts: %w", err)
	}
	// Set network
	s.network = network
	// Set contract addresses
	s.addresses = s.contracts.ContractsAddresses
	if organizationRegistryAddress != "" {
		s.addresses.OrganizationRegistry = common.HexToAddress(organizationRegistryAddress)
	}

	if processRegistryAddress != "" {
		s.addresses.ProcessRegistry = common.HexToAddress(processRegistryAddress)
	}

	if stateTransitionZKVerifierAddress != "" {
		s.addresses.StateTransitionZKVerifier = common.HexToAddress(stateTransitionZKVerifierAddress)
	}

	if resultsZKVerifierAddress != "" {
		s.addresses.ResultsZKVerifier = common.HexToAddress(resultsZKVerifierAddress)
	}

	// Load contracts from the default config
	if err = s.contracts.LoadContracts(s.addresses); err != nil {
		return fmt.Errorf("error loading contracts: %w", err)
	}
	// Add the web3rpcs to the contracts
	var rpcAdded bool
	for i := range rpcs {
		if err := s.contracts.AddWeb3Endpoint(rpcs[i]); err != nil {
			log.Warnw("failed to add endpoint", "rpc", rpcs[i], "err", err)
			continue
		}
		rpcAdded = true
	}
	if !rpcAdded {
		return fmt.Errorf("no valid web3 rpc endpoints available")
	}
	// Set the private key for the account
	if err := s.contracts.SetAccountPrivateKey(util.TrimHex(privKey)); err != nil {
		return fmt.Errorf("error setting account private key: %w", err)
	}
	log.Infow("contracts initialized", "chainId", s.contracts.ChainID)
	return nil
}

func (s *CLIServices) initSequencerCLI() error {
	sequencers := *sequencerEndpoints
	// If no sequencer endpoint is provided, start a local one
	if len(sequencers) == 0 {
		return fmt.Errorf("no sequencers provided")
	}
	// Create a API client
	var err error
	s.cli, err = client.New(sequencers[0])
	if err != nil {
		return fmt.Errorf("failed to create sequencer client: %w", err)
	}
	// Wait for the sequencer to be ready, make ping request until it responds
	pingCtx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	defer cancel()
	for isConnected := false; !isConnected; {
		select {
		case <-pingCtx.Done():
			return fmt.Errorf("timeout reached while connecting to sequencer")
		default:
			_, status, err := s.cli.Request(http.MethodGet, nil, nil, api.PingEndpoint)
			if err == nil && status == http.StatusOK {
				isConnected = true
				break
			}
			log.Warnw("failed to ping sequencer", "status", status, "err", err)
			time.Sleep(10 * time.Second)
		}
	}
	log.Info("connected to sequencer")

	return nil
}

func (s *CLIServices) CreateAccountOrganization() (common.Address, error) {
	orgAddr := s.contracts.AccountAddress()
	if _, err := s.contracts.Organization(orgAddr); err == nil {
		return orgAddr, nil // Organization already exists
	}
	// Create a new organization in the contracts
	txHash, err := s.contracts.CreateOrganization(orgAddr, &types.OrganizationInfo{
		Name:        fmt.Sprintf("Vocdoni test %x", orgAddr[:4]),
		MetadataURI: "https://vocdoni.io",
	})
	if err != nil {
		return common.Address{}, err
	}

	// Wait for the transaction to be mined
	if err := s.contracts.WaitTxByHash(txHash, time.Second*30); err != nil {
		return common.Address{}, err
	}

	return orgAddr, nil
}

func (s *CLIServices) CreateCensus(
	origin types.CensusOrigin,
	size int,
	weight uint64,
	c3URL string,
	privKey string,
) (types.HexBytes, string, []*ethereum.Signer, error) {
	signers := []*ethereum.Signer{}
	votes := []state.Vote{}
	if len(privKey) > 0 {
		signer, err := ethereum.NewSignerFromHex(privKey)
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to create signer from privkey: %w", err)
		}
		signers = append(signers, signer)
		votes = append(votes, state.Vote{
			Address: signer.Address().Big(),
			Weight:  new(big.Int).SetUint64(weight),
		})
		size -= 1
	}
	// Generate random participants
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
	censusRoot, censusURI, err := censustest.NewCensus3MerkleTreeForTest(s.ctx, types.CensusOriginMerkleTreeOffchainStaticV1, votes, c3URL)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to serve census merkle tree: %w", err)
	}
	return censusRoot, censusURI, signers, nil
}

func (s *CLIServices) CreateProcess(
	censusOrigin types.CensusOrigin,
	censusRoot types.HexBytes,
	censusURI string,
	ballotMode *types.BallotMode,
	maxVoters *types.BigInt,
) (types.ProcessID, *types.EncryptionKey, error) {
	// Create test process request

	processId, err := s.contracts.NextProcessID(s.contracts.AccountAddress())
	if err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to get next process ID: %v", err)
	}

	// Sign the process creation request
	signature, err := s.contracts.SignMessage(fmt.Appendf(nil, types.NewProcessMessageToSign, processId.String()))
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
			CensusOrigin: censusOrigin,
		},
	}
	body, code, err := s.cli.Request(http.MethodPost, process, nil, api.ProcessesEndpoint)
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
		OrganizationId: s.contracts.AccountAddress(),
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
			CensusOrigin: censusOrigin,
		},
	}
	// Create process in the contracts
	pid, txHash, err := s.contracts.CreateProcess(newProcess)
	if err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to create process in contracts: %v", err)
	}

	// Wait for the process creation transaction to be mined
	if err = s.contracts.WaitTxByHash(*txHash, time.Minute*2); err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to wait for process creation tx: %v", err)
	}

	// Wait for the process to be registered in the sequencer
	processCtx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	defer cancel()
	for processReady := false; !processReady; {
		select {
		case <-time.After(time.Second * 5):
			pBytes, status, err := s.cli.Request(http.MethodGet, nil, nil, api.EndpointWithParam(api.ProcessEndpoint, api.ProcessURLParam, pid.String()))
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
	return pid, encryptionKeys, nil
}

func (s *CLIServices) CreateVote(
	privKey *ethereum.Signer,
	pid types.ProcessID,
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
		ProcessID: pid,
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
		ProcessID:        &wasmResult.ProcessID,
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

func (s *CLIServices) SubmitVote(vote api.Vote) (types.HexBytes, error) {
	// Make the request to cast the vote
	body, status, err := s.cli.Request(http.MethodPost, vote, nil, api.VotesEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to cast vote: %w", err)
	} else if status != http.StatusOK {
		return nil, fmt.Errorf("failed to cast vote (status code %d): %s", status, body)
	}
	return vote.VoteID, nil
}
