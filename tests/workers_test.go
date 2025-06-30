package tests

import (
	"context"
	"net/http"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/types"
)

func TestWorkerIntegration(t *testing.T) {
	c := qt.New(t)
	workerTimeout := time.Second * 5
	testSeed := "test-seed"
	// Setup
	ctx := t.Context()
	services := NewTestService(t, ctx, testSeed, workerTimeout)
	_, port := services.API.HostPort()
	mainAPIUUID, err := api.WorkerSeedToUUID(testSeed)
	c.Assert(err, qt.IsNil)

	cli, err := NewTestClient(port)
	c.Assert(err, qt.IsNil)
	// Start sequencer batch time window
	services.Sequencer.SetBatchTimeWindow(time.Second * 120)
	var (
		pid           *types.ProcessID
		encryptionKey *types.EncryptionKey
		ballotMode    *types.BallotMode
		signers       []*ethereum.Signer
		proof         *types.CensusProof
		root          []byte
	)

	c.Run("create organization", func(c *qt.C) {
		orgAddr := createOrganization(c, services.Contracts)
		t.Logf("Organization address: %s", orgAddr.String())
	})

	c.Run("launch a ghost worker", func(c *qt.C) {
		getJobEndpoint := api.EndpointWithParam(api.WorkerGetJobEndpoint, api.WorkerUUIDParam, mainAPIUUID.String())
		getJobEndpoint = api.EndpointWithParam(getJobEndpoint, api.WorkerAddressParam, services.Contracts.AccountAddress().String())
		body, status, err := cli.Request(http.MethodGet, nil, nil, getJobEndpoint)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to get job: %v", err))
		c.Assert(status, qt.Equals, http.StatusNoContent, qt.Commentf("Expected 204 No Content, got %d: %s", status, string(body)))
		c.Log("Ghost worker job request successful, no job available yet")
	})

	c.Run("launch a ghost worker", func(c *qt.C) {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		// make a request to the api to get a job until get banned
		for {
			select {
			case <-ctx.Done():
				c.Fatal("Timeout waiting for ghost worker job request")
				c.FailNow()
			case <-ticker.C:
				getJobEndpoint := api.EndpointWithParam(api.WorkerGetJobEndpoint, api.WorkerUUIDParam, mainAPIUUID.String())
				getJobEndpoint = api.EndpointWithParam(getJobEndpoint, api.WorkerAddressParam, services.Contracts.AccountAddress().String())
				body, status, err := cli.Request(http.MethodGet, nil, nil, getJobEndpoint)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to get job: %v", err))
				c.Assert(status, qt.Equals, http.StatusNoContent, qt.Commentf("Expected 204 No Content, got %d: %s", status, string(body)))
				c.Log("Ghost worker job request successful, no job available yet")
			}
		}
	})

	c.Run("create process", func(c *qt.C) {
		// Create census with numBallot participants
		var participants []*api.CensusParticipant
		root, participants, signers = createCensus(c, cli, 1)
		c.Assert(participants, qt.Not(qt.IsNil))
		c.Assert(signers, qt.Not(qt.IsNil))
		c.Assert(len(participants), qt.Equals, 1)

		// Generate proof for first participant
		proof = generateCensusProof(c, cli, root, participants[0].Key)
		c.Assert(proof, qt.Not(qt.IsNil))
		c.Assert(proof.Siblings, qt.IsNotNil)
		// Check the first proof key is the same as the participant key and signer address
		qt.Assert(t, proof.Key.String(), qt.DeepEquals, participants[0].Key.String())
		qt.Assert(t, string(proof.Key), qt.DeepEquals, string(signers[0].Address().Bytes()))

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

		pid, encryptionKey = createProcess(c, services.Contracts, cli, root, *ballotMode)
		// create a timeout for the process creation, if it is greater than the test timeout
		// use the test timeout
		createProcessTimeout := time.Minute * 2
		if timeout, hasDeadline := t.Deadline(); hasDeadline {
			remainingTime := time.Until(timeout)
			if remainingTime < createProcessTimeout {
				createProcessTimeout = remainingTime
			}
		}
		// Wait for the process to be registered
		createProcessCtx, cancel := context.WithTimeout(ctx, createProcessTimeout)
		defer cancel()

	CreateProcessLoop:
		for {
			select {
			case <-createProcessCtx.Done():
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
			case <-createProcessCtx.Done():
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

	c.Run("send a vote", func(c *qt.C) {
		for _, signer := range signers {
			// generate a vote for the first participant
			vote, _ := createVote(c, pid, ballotMode, encryptionKey, signer, nil)
			// generate census proof for first participant
			censusProof := generateCensusProof(c, cli, root, signer.Address().Bytes())
			c.Assert(censusProof, qt.Not(qt.IsNil))
			c.Assert(censusProof.Siblings, qt.IsNotNil)
			vote.CensusProof = *censusProof
			// Make the request to cast the vote
			_, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, 200)
		}
	})
	time.Sleep(5 * time.Minute)
}
