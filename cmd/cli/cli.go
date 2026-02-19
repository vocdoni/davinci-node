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
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/api/client"
	censustest "github.com/vocdoni/davinci-node/census/test"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	ballotprooftest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/spec"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
	"github.com/vocdoni/davinci-node/util/circomgnark"
	"github.com/vocdoni/davinci-node/web3"
)

// CLIServices holds the services required for the CLI operations
type CLIServices struct {
	cli *client.HTTPclient

	contracts *web3.Contracts
	addresses *web3.Addresses
	network   string

	ctx    context.Context
	cancel context.CancelFunc
}

// NewCLIServices creates a new CLIServices instance
func NewCLIServices(ctx context.Context) *CLIServices {
	ctx, cancel := context.WithCancel(ctx)
	return &CLIServices{
		ctx:    ctx,
		cancel: cancel,
	}
}

// Init initializes the CLI services with the provided configuration. It sets
// up the web3 contracts and the sequencer client.
func (s *CLIServices) Init(
	network string,
	rpcs []string,
	consensusAPI string,
	processRegistryAddress string,
	stateTransitionZKVerifierAddress string,
	resultsZKVerifierAddress string,
	privKey string,
) error {
	if err := s.initContracts(
		network,
		rpcs,
		consensusAPI,
		processRegistryAddress,
		stateTransitionZKVerifierAddress,
		resultsZKVerifierAddress,
		privKey,
	); err != nil {
		return err
	}

	return s.initSequencerCLI()
}

func (s *CLIServices) initContracts(
	network string,
	rpcs []string,
	consensusAPI string,
	processRegistryAddress string,
	stateTransitionZKVerifierAddress string,
	resultsZKVerifierAddress string,
	privKey string,
) error {
	log.Infow("using web3 configuration",
		"network", network,
		"processRegistryAddress", processRegistryAddress,
		"stateTransitionZKVerifierAddress", stateTransitionZKVerifierAddress,
		"resultsZKVerifierAddress", resultsZKVerifierAddress,
		"web3rpcs", rpcs,
		"consensusAPI", consensusAPI,
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

// CreateCensus creates a census with the given parameters and returns the
// census root, census URI and the list of signers used to create the census.
// If privKey is provided, it will be used as the first participant in the
// census. If some error occurs, it will be returned.
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

// CreateProcess creates a new process with the given census and ballot mode.
// It returns the process ID and encryption key used for the process. If some
// error occurs, it will be returned.
func (s *CLIServices) CreateProcess(
	census *types.Census,
	ballotMode spec.BallotMode,
	maxVoters *types.BigInt,
) (types.ProcessID, *types.EncryptionKey, error) {
	// Make the request to create the encryption keys
	body, code, err := s.cli.Request(http.MethodPost, nil, nil, api.NewEncryptionKeysEndpoint)
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
		OrganizationID: s.contracts.AccountAddress(),
		EncryptionKey:  encryptionKeys,
		StartTime:      time.Now().Add(1 * time.Minute),
		Duration:       time.Hour,
		MetadataURI:    "https://example.com/metadata",
		BallotMode:     ballotMode,
		MaxVoters:      maxVoters,
		Census:         census,
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

// StopProcess stops the process with the given process ID by setting its
// status to Ended in the smart contracts. It sends a transaction to update
// the process status and waits for the transaction to be mined. If any error
// occurs during the process, it will be returned.
func (s *CLIServices) StopProcess(pid types.ProcessID) error {
	tx, err := s.contracts.SetProcessStatus(pid, types.ProcessStatusEnded)
	if err != nil {
		return fmt.Errorf("failed to stop process in contracts: %w", err)
	}
	return s.contracts.WaitTxByHash(*tx, time.Minute)
}

// ProcessEncKey retrieves the encryption key for the given process ID from
// the sequencer. It request the process information to the sequencer API and
// extracts the encryption key from the response. If any error occurs during
// the request or unmarshalling, it will be returned.
// The encryption key can be used to encrypt ballots for the specified process.
func (s *CLIServices) ProcessEncKey(pid types.ProcessID) (*types.EncryptionKey, error) {
	// Get the encryption keys from the sequencer
	processEndpoint := api.EndpointWithParam(api.ProcessEndpoint, api.ProcessURLParam, pid.String())
	log.Debugw("getting encryption keys",
		"pid", pid.String(),
		"endpoint", processEndpoint)
	processResponse, status, err := s.cli.Request(http.MethodGet, nil, nil, processEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get process info from sequencer: %v", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("failed to get process info from sequencer, status code: %d", status)
	}
	var processInfo api.ProcessResponse
	if err := json.Unmarshal(processResponse, &processInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal process info: %v", err)
	}
	return processInfo.EncryptionKey, nil
}

// VoterWeight retrieves the voter weight for a given process ID and address
// from the sequencer. It makes a request to the census participant endpoint
// of the sequencer API and extracts the weight from the response. If any
// error occurs during the request or unmarshalling, it will be returned.
func (s *CLIServices) VoterWeight(pid types.ProcessID, addr common.Address) (*types.BigInt, error) {
	participantEndpoint := api.EndpointWithParam(api.CensusParticipantEndpoint, api.ProcessURLParam, pid.String())
	participantEndpoint = api.EndpointWithParam(participantEndpoint, api.AddressURLParam, addr.Hex())

	participantResponse, status, err := s.cli.Request(http.MethodGet, nil, nil, participantEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get participant info from sequencer: %v", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("failed to get participant info from sequencer, status code: %d", status)
	}
	var participantInfo api.CensusParticipant
	if err := json.Unmarshal(participantResponse, &participantInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal participant info: %v", err)
	}
	return participantInfo.Weight, nil
}

// CreateVote creates a new vote for the given process ID and ballot mode. It
// generates a random ballot based on the ballot mode and constructs the vote
// structure with the necessary fields, including the ballot proof and
// signature. If any error occurs during the process, it will be returned.
func (s *CLIServices) CreateVote(
	privKey *ethereum.Signer,
	pid types.ProcessID,
	bm spec.BallotMode,
) (api.Vote, error) {
	// Fetch the encryption key for the process
	encKey, err := s.ProcessEncKey(pid)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to get encryption key for process %s: %v", pid.String(), err)
	}

	// Get voter address
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	k, err := specutil.RandomK()
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate random k: %v", err)
	}

	// Get voter weight
	weight, err := s.VoterWeight(pid, address)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to get voter weight: %v", err)
	}

	// Generate random ballot fields based on the ballot mode
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
		ProcessID: pid,
		EncryptionKey: []*types.BigInt{
			(*types.BigInt)(encKey.X),
			(*types.BigInt)(encKey.Y),
		},
		K:           (*types.BigInt)(k),
		BallotMode:  bm,
		Weight:      weight,
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
			Weight: weight,
		},
	}, nil
}

// SubmitVote submits the given vote to the sequencer API. It makes a POST
// request to the votes endpoint with the vote data. If the request is
// successful, it returns the vote ID. If any error occurs during the request
// or if the response status code is not OK, it will be returned.
func (s *CLIServices) SubmitVote(vote api.Vote) (types.VoteID, error) {
	// Make the request to cast the vote
	body, status, err := s.cli.Request(http.MethodPost, vote, nil, api.VotesEndpoint)
	if err != nil {
		return 0, fmt.Errorf("failed to cast vote: %w", err)
	} else if status != http.StatusOK {
		return 0, fmt.Errorf("failed to cast vote (status code %d): %s", status, body)
	}
	return vote.VoteID, nil
}
