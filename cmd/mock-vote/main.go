package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common"
	flag "github.com/spf13/pflag"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/api/client"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	ballotprooftest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/sequencer"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

const (
	defaultNetwork       = "sep"
	defaultSequencerHost = "0.0.0.0"
	defaultSequencerPort = 8080
)

var (
	defaultSequencerEndpoint = fmt.Sprintf("http://%s:%d", defaultSequencerHost, defaultSequencerPort)
	defaultContracts         = config.DefaultConfig[defaultNetwork]
)

func main() {
	// define cli flags
	var (
		web3rpcs                         = flag.StringSlice("web3rpcs", nil, "web3 rpc http endpoints")
		organizationRegistryAddress      = flag.String("organizationRegistryAddress", defaultContracts.OrganizationRegistrySmartContract, "organization registry smart contract address")
		processRegistryAddress           = flag.String("processRegistryAddress", defaultContracts.ProcessRegistrySmartContract, "process registry smart contract address")
		stateTransitionZKVerifierAddress = flag.String("stateTransitionZKVerifierAddress", defaultContracts.StateTransitionZKVerifier, "state transition zk verifier smart contract address")
		resultsZKVerifierAddress         = flag.String("resultsZKVerifierAddress", defaultContracts.ResultsZKVerifier, "state transition zk verifier smart contract address")
		testTimeout                      = flag.Duration("timeout", 20*time.Minute, "timeout for the test")
		sequencerEndpoint                = flag.String("sequencerEndpoint", defaultSequencerEndpoint, "sequencer endpoint")
		ballotInputsJSON                 = flag.String("ballotInputsJSON", "", "path to the ballot inputs JSON file")
		privKey                          = flag.String("privkey", "", "private key to use for the Ethereum account")
		censusRoot                       = flag.String("censusRoot", "", "census root to use for the test")
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

	// Check if the ballot inputs JSON file is provided
	if *ballotInputsJSON == "" {
		log.Error("ballot inputs JSON file is required")
		flag.Usage()
		return
	}

	// If the ballot inputs JSON file is provided, load it
	var ballotInputs *ballotproof.BallotProofInputs
	if err := json.Unmarshal([]byte(*ballotInputsJSON), &ballotInputs); err != nil {
		log.Errorw(err, "failed to unmarshal ballot inputs JSON")
		flag.Usage()
		return
	}

	// If no web3rpcs are provided, use the default ones from chainlist
	var err error
	if len(*web3rpcs) == 0 {
		log.Error("no web3rpcs provided, please provide at least one web3 rpc endpoint")
		flag.Usage()
		return
	}

	log.Infow("using web3 configuration",
		"organizationRegistryAddress", *organizationRegistryAddress,
		"processRegistryAddress", *processRegistryAddress,
		"stateTransitionZKVerifierAddress", *stateTransitionZKVerifierAddress,
		"resultsZKVerifierAddress", *resultsZKVerifierAddress,
		"web3rpcs", *web3rpcs,
	)

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

	// If no sequencer endpoint is provided, start a local one
	if *sequencerEndpoint == defaultSequencerEndpoint {
		log.Infow("no remote sequencer endpoint provided, starting a local one...")
		// Start a local sequencer
		service := new(localService)
		if err := service.Start(testCtx, contracts, defaultNetwork); err != nil {
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

	voterSigner, err := ethereum.NewSignerFromHex(*privKey)
	if err != nil {
		log.Errorw(err, "failed to create voter signer")
		return
	}
	// Generate a vote
	vote, err := createVote(voterSigner, ballotInputs)
	if err != nil {
		log.Errorw(err, "failed to create vote")
		return
	}
	log.Infow("vote created", "vote", vote)

	// Generate a census proof for each participant
	root := types.HexStringToHexBytesMustUnmarshal(*censusRoot)
	vote.CensusProof, err = generateCensusProof(cli, root, vote.Address)
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
	log.Infow("vote sent", "voteID", voteID.String())

	// Wait for the votes to be registered in the smart contract
	log.Info("all votes sent, waiting for votes to be registered in smart contract...")
	newVotesCh := make(chan int)
	newVotesCtx, cancel := context.WithCancel(testCtx)
	defer cancel()
	go func() {
		for newVoteCount := range newVotesCh {
			log.Infow("vote count registered in smart contract", "voteCount", newVoteCount)
			// Check if the vote count is equal to the number of votes sent
			if newVoteCount >= 1 {
				cancel()
				break
			}
		}
	}()
	pid := new(types.ProcessID).SetBytes(ballotInputs.ProcessID)
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
	api            *service.APIService
}

func (s *localService) Start(ctx context.Context, contracts *web3.Contracts, network string) error {
	// Create storage with a in-memory database
	stg := storage.New(memdb.New())
	sequencer.AggregatorTickerInterval = time.Second * 2
	sequencer.NewProcessMonitorInterval = time.Second * 5
	// Monitor new processes from the contracts
	s.processMonitor = service.NewProcessMonitor(contracts, stg, time.Second*2)
	if err := s.processMonitor.Start(ctx); err != nil {
		return fmt.Errorf("failed to start process monitor: %v", err)
	}
	// Start sequencer service
	s.sequencer = service.NewSequencer(stg, contracts, time.Second*30, nil)
	if err := s.sequencer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sequencer: %v", err)
	}
	// Start API service
	s.api = service.NewAPI(stg, defaultSequencerHost, defaultSequencerPort, network, false)
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
}

func createVote(
	privKey *ethereum.Signer,
	ballotInputs *ballotproof.BallotProofInputs,
) (api.Vote, error) {
	// Generate the inputs for the ballot proof circuit
	wasmResult, err := ballotproof.GenerateBallotProofInputs(ballotInputs)
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
		Address:          ballotInputs.Address,
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
		return nil, fmt.Errorf("failed to cast vote, status code: %d: %s", status, string(body))
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
