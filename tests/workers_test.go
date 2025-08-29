package tests

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/workers"
)

const (
	testWorkerSeed            = "test-seed"
	testWorkerTokenExpiration = 24 * time.Hour // 90 days
	testWorkerTimeout         = time.Second * 5
)

func TestWorkerIntegration(t *testing.T) {
	if enabled := os.Getenv("WORKER_INTEGRATION_TEST"); enabled != "1" || enabled == "true" || enabled == "TRUE" {
		t.Skip("Skipping worker integration test, set WORKER_INTEGRATION_TEST=1 to run it")
	}

	c := qt.New(t)
	numBallots := 20                     // number of ballots to be sent in the process
	failedJobsToGetBanned := 3           // number of failed jobs to get banned
	workerBanTimeout := 30 * time.Second // timeout for worker ban
	// Setup
	ctx := t.Context()
	services := NewTestService(t, ctx, testWorkerSeed, testWorkerTokenExpiration, testWorkerTimeout, &workers.WorkerBanRules{
		BanTimeout:          workerBanTimeout,
		FailuresToGetBanned: failedJobsToGetBanned,
	})
	_, port := services.API.HostPort()

	cli, err := NewTestClient(port)
	c.Assert(err, qt.IsNil)

	// get the worker sign message from the API
	body, status, err := cli.Request(http.MethodGet, nil, nil, api.WorkerTokenDataEndpoint)
	c.Assert(err, qt.IsNil)
	c.Assert(status, qt.Equals, http.StatusOK)
	// decode the sign message
	var signMessageResponse api.WorkerAuthDataResponse
	c.Assert(json.Unmarshal(body, &signMessageResponse), qt.IsNil)

	var (
		pid           *types.ProcessID
		encryptionKey *types.EncryptionKey
		stateRoot     *types.HexBytes
		ballotMode    *types.BallotMode
		censusRoot    []byte
	)

	// create the worker signer
	workerSigner, err := ethereum.NewSigner()
	c.Assert(err, qt.IsNil)
	workerName := "test-worker"
	workerAddr := workerSigner.Address().Hex()
	workerSignature, err := workerSigner.Sign([]byte(signMessageResponse.Message))
	c.Assert(err, qt.IsNil)

	c.Run("launch a worker with no jobs pending", func(c *qt.C) {
		getJobEndpoint := api.EndpointWithParam(api.WorkerJobEndpoint, api.WorkerAddressQueryParam, workerAddr)
		getJobEndpoint = api.EndpointWithParam(getJobEndpoint, api.WorkerTokenQueryParam, workerSignature.String())
		body, status, err := cli.Request(http.MethodGet, nil, []string{api.WorkerNameQueryParam, workerName}, getJobEndpoint)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to get job: %v", err))
		c.Assert(status, qt.Equals, http.StatusNoContent, qt.Commentf("Expected 204 No Content, got %d: %s", status, string(body)))
		c.Log("Ghost worker job request successful, no job available yet")
	})

	var timeoutCh <-chan time.Time
	if deadline, hasDeadline := t.Deadline(); hasDeadline {
		remainingTime := time.Until(deadline)
		timeoutCh = time.After(remainingTime)
	} else {
		timeoutCh = time.After(10 * time.Minute)
	}

	var signers []*ethereum.Signer
	c.Run("create process", func(c *qt.C) {
		// Create census with numBallot participants
		var participants []*api.CensusParticipant
		censusRoot, participants, signers = createCensus(c, cli, numBallots)
		c.Assert(participants, qt.Not(qt.IsNil))
		c.Assert(signers, qt.Not(qt.IsNil))
		c.Assert(len(participants), qt.Equals, numBallots)

		ballotMode = &types.BallotMode{
			NumFields:      circuits.MockNumFields,
			UniqueValues:   circuits.MockUniqueValues == 1,
			MaxValue:       new(types.BigInt).SetUint64(circuits.MockMaxValue),
			MinValue:       new(types.BigInt).SetUint64(circuits.MockMinValue),
			MaxValueSum:    new(types.BigInt).SetUint64(circuits.MockMaxValueSum),
			MinValueSum:    new(types.BigInt).SetUint64(circuits.MockMinValueSum),
			CostFromWeight: circuits.MockCostFromWeight == 1,
			CostExponent:   circuits.MockCostExponent,
		}
		pid, encryptionKey, stateRoot = createProcessInSequencer(c, services.Contracts, cli, types.CensusOriginMerkleTree, censusRoot, ballotMode)
		pid2 := createProcessInContracts(c, services.Contracts, types.CensusOriginMerkleTree, censusRoot, ballotMode, encryptionKey, stateRoot)
		c.Assert(pid2.String(), qt.Equals, pid.String())

		// Wait for the process to be registered
	CreateProcessLoop:
		for {
			select {
			case <-timeoutCh:
				c.Fatal("Timeout waiting for process to be created and registered")
				c.FailNow()
			default:
				if _, err := services.Storage.Process(pid); err == nil {
					break CreateProcessLoop
				}
				time.Sleep(time.Millisecond * 200)
			}
		}
		t.Logf("Process ID: %s", pid.String())

		// Wait for the process to be registered in the sequencer
		for {
			select {
			case <-timeoutCh:
				c.Fatal("Timeout waiting for process to be registered in sequencer")
				c.FailNow()
			default:
				if services.Sequencer.ExistsProcessID(pid.Marshal()) {
					t.Logf("Process ID %s registered in sequencer", pid.String())
					return
				}
				time.Sleep(time.Millisecond * 200)
			}
		}
	})

	var voteIDs []types.HexBytes
	c.Run("send votes", func(c *qt.C) {
		for _, signer := range signers {
			// generate a vote for the first participant
			vote := createVoteWithRandomFields(c, pid, ballotMode, encryptionKey, signer, nil)
			// generate census proof for first participant
			censusProof := generateCensusProof(c, cli, censusRoot, pid.Marshal(), signer.Address().Bytes())
			c.Assert(censusProof, qt.Not(qt.IsNil))
			c.Assert(censusProof.Siblings, qt.IsNotNil)
			vote.CensusProof = *censusProof
			// Make the request to cast the vote
			_, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, 200)
			voteIDs = append(voteIDs, vote.VoteID)
		}
	})

	c.Run("ban worker", func(c *qt.C) {
	BanLoop:
		for {
			select {
			case <-timeoutCh:
				c.Fatal("Timeout waiting for ghost worker to be banned")
				c.FailNow()
				return
			default:
				// make a request to the api to get a job until get banned
				getJobEndpoint := api.EndpointWithParam(api.WorkerJobEndpoint, api.WorkerAddressQueryParam, workerAddr)
				getJobEndpoint = api.EndpointWithParam(getJobEndpoint, api.WorkerTokenQueryParam, workerSignature.String())
				body, status, err := cli.Request(http.MethodGet, nil, []string{api.WorkerNameQueryParam, workerName}, getJobEndpoint)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to get job: %v", err))
				if status == http.StatusOK || status == http.StatusNoContent {
					continue
				} else if status == http.StatusBadRequest && strings.Contains(string(body), "worker busy") {
					// wait for the requested job timeout to request again
					time.Sleep(testWorkerTimeout)
					continue
				}
				c.Assert(status, qt.Equals, http.StatusBadRequest, qt.Commentf("Expected 400 Bad Request, got %d", status))
				c.Assert(string(body), qt.Contains, "worker banned", qt.Commentf("Expected worker to be banned: %s", string(body)))
				break BanLoop
			}
		}
	})

	c.Run("unbar worker", func(c *qt.C) {
	UnbanLoop:
		for {
			select {
			case <-timeoutCh:
				c.Fatal("Timeout waiting for ghost worker to be banned")
				c.FailNow()
				return
			default:
				// make a request to the api to get a job until get unbanned
				getJobEndpoint := api.EndpointWithParam(api.WorkerJobEndpoint, api.WorkerAddressQueryParam, workerAddr)
				getJobEndpoint = api.EndpointWithParam(getJobEndpoint, api.WorkerTokenQueryParam, workerSignature.String())
				body, status, err := cli.Request(http.MethodGet, nil, []string{api.WorkerNameQueryParam, workerName}, getJobEndpoint)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to get job: %v", err))
				if status == http.StatusBadRequest || strings.Contains(string(body), "worker banned") {
					time.Sleep(workerBanTimeout)
					continue
				}
				c.Assert(status, qt.Equals, http.StatusOK, qt.Commentf("Expected 200 OK, got %d", status))
				break UnbanLoop
			}
		}
	})
}
