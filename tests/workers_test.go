package tests

import (
	"fmt"
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
	"github.com/vocdoni/davinci-node/util"
	"github.com/vocdoni/davinci-node/workers"
)

func TestWorkerIntegration(t *testing.T) {
	if enabled := os.Getenv("WORKER_INTEGRATION_TEST"); enabled != "1" || enabled == "true" || enabled == "TRUE" {
		t.Skip("Skipping worker integration test, set WORKER_INTEGRATION_TEST=1 to run it")
	}

	c := qt.New(t)
	numBallots := 20                     // number of ballots to be sent in the process
	testSeed := "test-seed"              // seed for the workers UUID of main sequencer
	workerTimeout := 5 * time.Second     // timeout for worker jobs
	failedJobsToGetBanned := 3           // number of failed jobs to get banned
	workerBanTimeout := 30 * time.Second // timeout for worker ban
	// Setup
	ctx := t.Context()
	services := NewTestService(t, ctx, testSeed, workerTimeout, &workers.WorkerBanRules{
		BanTimeout:          workerBanTimeout,
		FailuresToGetBanned: failedJobsToGetBanned,
	})
	_, port := services.API.HostPort()
	mainAPIUUID, err := workers.WorkerSeedToUUID(testSeed)
	c.Assert(err, qt.IsNil)

	cli, err := NewTestClient(port)
	c.Assert(err, qt.IsNil)
	// Start sequencer batch time window
	services.Sequencer.SetBatchTimeWindow(time.Second * 120)
	var (
		pid           *types.ProcessID
		encryptionKey *types.EncryptionKey
		stateRoot     *types.HexBytes
		ballotMode    *types.BallotMode
		censusRoot    []byte
	)

	workerName := "test-worker"
	workerAddr := fmt.Sprintf("0x%s", util.RandomHex(20))
	c.Run("launch a worker with no jobs pending", func(c *qt.C) {
		getJobEndpoint := api.EndpointWithParam(api.WorkerGetJobEndpoint, api.WorkerUUIDParam, mainAPIUUID.String())
		getJobEndpoint = api.EndpointWithParam(getJobEndpoint, api.WorkerAddressParam, workerAddr)
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
			MaxCount:        circuits.MockMaxCount,
			ForceUniqueness: circuits.MockForceUniqueness == 1,
			MaxValue:        new(types.BigInt).SetUint64(circuits.MockMaxValue),
			MinValue:        new(types.BigInt).SetUint64(circuits.MockMinValue),
			MaxTotalCost:    new(types.BigInt).SetUint64(circuits.MockMaxTotalCost),
			MinTotalCost:    new(types.BigInt).SetUint64(circuits.MockMinTotalCost),
			CostFromWeight:  circuits.MockCostFromWeight == 1,
			CostExponent:    circuits.MockCostExp,
		}
		pid, encryptionKey, stateRoot = createProcessInSequencer(c, services.Contracts, cli, censusRoot, ballotMode)
		pid2 := createProcessInContracts(c, services.Contracts, censusRoot, ballotMode, encryptionKey, stateRoot)
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
			vote, _ := createVote(c, pid, ballotMode, encryptionKey, signer, nil)
			// generate census proof for first participant
			censusProof := generateCensusProof(c, cli, censusRoot, signer.Address().Bytes())
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
				getJobEndpoint := api.EndpointWithParam(api.WorkerGetJobEndpoint, api.WorkerUUIDParam, mainAPIUUID.String())
				getJobEndpoint = api.EndpointWithParam(getJobEndpoint, api.WorkerAddressParam, workerAddr)
				body, status, err := cli.Request(http.MethodGet, nil, []string{api.WorkerNameQueryParam, workerName}, getJobEndpoint)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to get job: %v", err))
				if status == http.StatusOK || status == http.StatusNoContent {
					continue
				} else if status == http.StatusBadRequest && strings.Contains(string(body), "worker busy") {
					// wait for the requested job timeout to request again
					time.Sleep(workerTimeout)
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
				getJobEndpoint := api.EndpointWithParam(api.WorkerGetJobEndpoint, api.WorkerUUIDParam, mainAPIUUID.String())
				getJobEndpoint = api.EndpointWithParam(getJobEndpoint, api.WorkerAddressParam, workerAddr)
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
