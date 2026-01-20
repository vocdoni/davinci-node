package main

import (
	"context"
	"fmt"
	"time"

	flag "github.com/spf13/pflag"
	npbindings "github.com/vocdoni/davinci-contracts/golang-types"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

const (
	defaultNetwork     = "sepolia"
	defaultCAPI        = "https://ethereum-sepolia-beacon-api.publicnode.com"
	localSequencerHost = "0.0.0.0"
	localSequencerPort = 8080
	defaultCensus3URL  = "https://c3-dev.davinci.vote"
)

var (
	userWeight = uint64(testutil.Weight)
	ballotMode = testutil.BallotModeInternal()

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
	cOrigin                          = flag.String("censusOrigin", types.CensusOriginMerkleTreeOffchainStaticV1.String(), "census origin to use")
	cRoot                            = flag.BytesHex("censusRoot", nil, "census root to use (if empty, a new census will be created)")
	cURI                             = flag.String("censusURI", "", "census URI to use (if empty, a new census will be created)")
	votersCount                      = flag.Int("votersCount", 10, "number of voters that will cast a vote (half of them will rewrite it)")
	voteSleepTime                    = flag.Duration("voteSleepTime", 10*time.Second, "time to sleep between votes")
	web3Network                      = flag.StringP("web3.network", "n", defaultNetwork, fmt.Sprintf("network to use %v", npbindings.AvailableNetworksByName))
	voterPrivkey                     = flag.String("voterPrivkey", "", "private key to use for the voter account")
)

func main() {
	flag.Parse()
	log.Init("debug", "stdout", nil)

	ctx, cancel := context.WithTimeout(context.Background(), *testTimeout)
	defer cancel()

	// Initialize CLI services
	cliSrv := NewCLIServices(ctx)
	if err := cliSrv.Init(
		*web3Network,
		*web3rpcs,
		*consensusAPI,
		*organizationRegistryAddress,
		*processRegistryAddress,
		*stateTransitionZKVerifierAddress,
		*resultsZKVerifierAddress,
		*privKey,
	); err != nil {
		log.Fatalf("failed to initialize web3 contracts: %w", err)
	}

	// Create a new organization
	organizationAddr, err := cliSrv.CreateAccountOrganization()
	if err != nil {
		log.Errorw(err, "failed to create organization")
		log.Warn("check if the organization is already created or the account has enough funds")
		return
	}
	log.Infow("organization ready", "address", organizationAddr.Hex())

	censusOrigin := types.CensusOriginFromString(*cOrigin)
	if !censusOrigin.Valid() {
		log.Errorw(fmt.Errorf("invalid census origin: %s", *cOrigin), "failed to create census")
		return
	}

	var (
		censusRoot types.HexBytes
		censusURI  string
		signers    []*ethereum.Signer
	)
	if cRoot == nil || len(*cURI) == 0 {
		// Create a new census with numBallot participants
		censusRoot, censusURI, signers, err = cliSrv.CreateCensus(censusOrigin, *votersCount, userWeight, *census3URL, *voterPrivkey)
		if err != nil {
			log.Errorw(err, "failed to create census")
			return
		}
		log.Infow("census created",
			"root", censusRoot.String(),
			"size", len(signers))
	} else {
		censusRoot = *cRoot
		censusURI = *cURI
	}
	log.Debugw("census parameters",
		"origin", censusOrigin.String(),
		"root", censusRoot.String(),
		"uri", censusURI)

	// Create a new process with mocked ballot mode
	pid, encryptionKey, err := cliSrv.CreateProcess(censusOrigin, censusRoot, censusURI, ballotMode, new(types.BigInt).SetInt(*votersCount))
	if err != nil {
		log.Errorw(err, "failed to create process")
		return
	}
	log.Infow("process created", "pid", pid.String())

	if voterPrivkey != nil && len(*voterPrivkey) > 0 {
		voterSigner, err := ethereum.NewSignerFromHex(*voterPrivkey)
		if err != nil {
			log.Errorw(err, "failed to create voter signer")
			return
		}
		vote, err := cliSrv.CreateVote(voterSigner, pid, encryptionKey, ballotMode)
		if err != nil {
			log.Errorw(err, "failed to create vote")
			return
		}
		voteID, err := cliSrv.SubmitVote(vote)
		if err != nil {
			log.Errorw(err, "failed to submit vote")
			return
		}
		log.Infow("vote submitted", "voteID", voteID.String(), "pid", pid.String())
	}
}
