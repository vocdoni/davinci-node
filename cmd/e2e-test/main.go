package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"

	flag "github.com/spf13/pflag"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/vocdoni-z-sandbox/api"
	"github.com/vocdoni/vocdoni-z-sandbox/api/client"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/config"
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
	// first account private key created by anvil with default mnemonic
	testLocalAccountPrivKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
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
		privKey           = flag.String("privkey", testLocalAccountPrivKey, "private key to use for the Ethereum account")
		deployContracts   = flag.Bool("deployContracts", false, "define if contracts should be deployed or not, if not, it will use the ones already deployed and defined in config/contracts.go")
		web3rpcs          = flag.StringSlice("web3rpcs", nil, "web3 rpc http endpoints")
		testTimeout       = flag.Duration("timeout", 10*time.Minute, "timeout for the test")
		sequencerEndpoint = flag.String("sequencerEndpoint", defaultSequencerEndpoint, "sequencer endpoint")
		createOrg         = flag.Bool("createOrganization", true, "create a new organization")
		// voteCount         = flag.Int("voteCount", 10, "number of votes to cast")
		// voteSleepTime     = flag.Duration("voteSleepTime", 10*time.Second, "time to sleep between votes")
	)
	flag.Parse()

	log.Init("debug", "stdout", nil)

	testCtx, cancel := context.WithTimeout(context.Background(), *testTimeout)
	defer cancel()

	var err error
	contracts := &web3.Contracts{}
	*privKey = util.TrimHex(*privKey)

	// If no web3rpcs are provided, use the default ones from chainlist
	if len(*web3rpcs) == 0 {
		if *web3rpcs, err = chainlist.EndpointList(defaultNetwork, 10); err != nil {
			log.Fatal(err)
		}
	}
	// If the contracts should be deployed, deploy them, if not use the ones
	// already deployed and defined in config/contracts.go
	if *deployContracts {
		log.Fatalf("deploying contracts is not supported yet")
		return
	} else {
		// Intance contracts with the provided web3rpcs
		contracts, err = web3.New(*web3rpcs)
		if err != nil {
			log.Fatal(err)
		}
		// Load contracts from the default config
		if err = contracts.LoadContracts(&web3.Addresses{
			OrganizationRegistry:      common.HexToAddress(defaultContracts.OrganizationRegistrySmartContract),
			ProcessRegistry:           common.HexToAddress(defaultContracts.ProcessRegistrySmartContract),
			ResultsRegistry:           common.HexToAddress(defaultContracts.ResultsSmartContract),
			StateTransitionZKVerifier: common.HexToAddress(defaultContracts.StateTransitionZKVerifier),
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
	}

	// If no sequencer endpoint is provided, start a local one
	if *sequencerEndpoint == defaultSequencerEndpoint {
		// Create storage with a in-memory database
		stg := storage.New(memdb.New())
		sequencer.AggregatorTickerInterval = time.Second * 2
		sequencer.NewProcessMonitorInterval = time.Second * 5
		vp := service.NewSequencer(stg, contracts, time.Second*30)
		if err := vp.Start(testCtx); err != nil {
			log.Fatal(err)
		}
		// Monitor new processes from the contracts
		pm := service.NewProcessMonitor(contracts, stg, time.Second*2)
		if err := pm.Start(testCtx); err != nil {
			log.Fatal(err)
		}
		// Start finalizer service
		fin := service.NewFinalizer(stg, stg.StateDB(), time.Second*5)
		if err := fin.Start(testCtx, time.Second*5); err != nil {
			log.Fatal(err)
		}
		// Start API service
		api := service.NewAPI(stg, defaultSequencerHost, defaultSequencerPort, defaultNetwork)
		if err := api.Start(testCtx); err != nil {
			log.Fatal(err)
		}
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
			return
		}
		log.Infow("organization created", "address", organizationAddr.Hex())
	} else {
		log.Infow("skipping organization creation")
	}

	// Create a new census with numBallot participants
	censusRoot, participants, signers, err := createCensus(cli, 10)
	if err != nil {
		log.Errorw(err, "failed to create census")
	}

	// Create a new process with mocked ballot mode
	pid, encryptionKey, err := createProcess(testCtx, contracts, cli, censusRoot, mockedBallotMode)
	if err != nil {
		log.Errorw(err, "failed to create process")
		return
	}
	log.Infow("process created", "pid", pid.String())

	// TODO: remove this
	_ = participants
	_ = signers
	_ = encryptionKey
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

func createCensus(cli *client.HTTPclient, size int) ([]byte, []*api.CensusParticipant, []*ethereum.Signer, error) {
	// Request a new census
	body, code, err := cli.Request(http.MethodPost, nil, nil, api.NewCensusEndpoint)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to request new census: %v", err)
	} else if code != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("failed to request new census, status code: %d", code)
	}

	// Decode census response
	var resp api.NewCensus
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&resp); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode census response: %v", err)
	}

	// Generate random participants
	signers := []*ethereum.Signer{}
	censusParticipants := api.CensusParticipants{Participants: []*api.CensusParticipant{}}
	for range size {
		signer, err := ethereum.NewSigner()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to create signer: %v", err)
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
		return nil, nil, nil, fmt.Errorf("failed to add participants to census: %v", err)
	} else if code != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("failed to add participants to census, status code: %d", code)
	}

	// Get census root
	getRootEnpoint := api.EndpointWithParam(api.GetCensusRootEndpoint, api.CensusURLParam, resp.Census.String())
	body, code, err = cli.Request(http.MethodGet, nil, nil, getRootEnpoint)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get census root: %v", err)
	} else if code != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("failed to get census root, status code: %d", code)
	}

	// Decode census root
	var rootResp api.CensusRoot
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&rootResp); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode census root response: %v", err)
	}

	return rootResp.Root, censusParticipants.Participants, signers, nil
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
