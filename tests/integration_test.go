package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/vocdoni-z-sandbox/api"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/signatures/ethereum"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/sequencer"
	"github.com/vocdoni/vocdoni-z-sandbox/service"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

func TestMain(m *testing.M) {
	log.Init(log.LogLevelDebug, "stdout", nil)
	if err := service.DownloadArtifacts(30*time.Minute, ""); err != nil {
		log.Errorw(err, "failed to download artifacts")
		return
	}
	os.Exit(m.Run())
}

func TestIntegration(t *testing.T) {
	numBallots := 5
	c := qt.New(t)

	// Setup
	ctx := context.Background()
	services := NewTestService(t, ctx)
	_, port := services.API.HostPort()
	cli, err := NewTestClient(port)
	c.Assert(err, qt.IsNil)

	// Start sequencer batch time window
	services.Sequencer.SetBatchTimeWindow(time.Second * 50)

	if os.Getenv("DEBUG") != "" && os.Getenv("DEBUG") != "false" {
		// Create a debug prover that will debug circuit execution during testing
		services.Sequencer.SetProver(sequencer.NewDebugProver(t))
	} else {
		t.Log("Debug prover is disabled! Set DEBUG=true to enable it.")
	}

	var (
		pid           *types.ProcessID
		encryptionKey *types.EncryptionKey
		ballotMode    *types.BallotMode
		signers       []*ethereum.Signer
		proofs        []*types.CensusProof
		root          []byte
		participants  []*api.CensusParticipant
	)

	c.Run("create organization", func(c *qt.C) {
		orgAddr := createOrganization(c, services.Contracts)
		t.Logf("Organization address: %s", orgAddr.String())
	})

	c.Run("create process", func(c *qt.C) {
		// Create census with numBallot participants
		root, participants, signers = createCensus(c, cli, numBallots)

		// Generate proof for first participant
		proofs = make([]*types.CensusProof, numBallots)
		for i := range participants {
			proofs[i] = generateCensusProof(c, cli, root, participants[i].Key)
			c.Assert(proofs[i], qt.Not(qt.IsNil))
			c.Assert(proofs[i].Siblings, qt.IsNotNil)
		}
		// Check the first proof key is the same as the participant key and signer address
		qt.Assert(t, proofs[0].Key.String(), qt.DeepEquals, participants[0].Key.String())
		qt.Assert(t, string(proofs[0].Key), qt.DeepEquals, string(signers[0].Address().Bytes()))

		mockMode := circuits.MockBallotMode()
		ballotMode = &types.BallotMode{
			MaxCount:        uint8(mockMode.MaxCount.Uint64()),
			ForceUniqueness: mockMode.ForceUniqueness.Uint64() == 1,
			MaxValue:        (*types.BigInt)(mockMode.MaxValue),
			MinValue:        (*types.BigInt)(mockMode.MinValue),
			MaxTotalCost:    (*types.BigInt)(mockMode.MaxTotalCost),
			MinTotalCost:    (*types.BigInt)(mockMode.MinTotalCost),
			CostFromWeight:  mockMode.CostFromWeight.Uint64() == 1,
			CostExponent:    uint8(mockMode.CostExp.Uint64()),
		}

		pid, encryptionKey = createProcess(c, services.Contracts, cli, root, *ballotMode)

		// Wait for the process to be registered
		for {
			if _, err := services.Storage.Process(pid); err == nil {
				break
			}
			time.Sleep(time.Millisecond * 200)
		}
		t.Logf("Process ID: %s", pid.String())

		// Wait for the process to be registered in the sequencer
		for !services.Sequencer.ExistsProcessID(pid.Marshal()) {
			time.Sleep(time.Millisecond * 200)
		}
	})

	// Store the voteIDs returned from the API to check their status later
	var voteIDs []types.HexBytes

	c.Run("create votes", func(c *qt.C) {
		count := 0
		for i := range signers {
			// generate a vote for the first participant
			vote := createVote(c, pid, ballotMode, encryptionKey, signers[i])
			// generate census proof for first participant
			censusProof := generateCensusProof(c, cli, root, signers[i].Address().Bytes())
			c.Assert(censusProof, qt.Not(qt.IsNil))
			c.Assert(censusProof.Siblings, qt.IsNotNil)
			vote.CensusProof = *censusProof

			// Make the request to cast the vote
			body, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, 200)

			// Parse the response body to get the vote ID
			var voteResponse api.VoteResponse
			err = json.NewDecoder(bytes.NewReader(body)).Decode(&voteResponse)
			c.Assert(err, qt.IsNil)
			c.Assert(voteResponse.VoteID, qt.Not(qt.IsNil))
			c.Logf("Vote %d created with ID: %s", i, voteResponse.VoteID.String())

			// Save the voteID for status checks
			voteIDs = append(voteIDs, voteResponse.VoteID)
			count++
		}
		c.Assert(count, qt.Equals, numBallots)
		// Wait for the vote to be registered
		t.Logf("Waiting for %d votes to be registered and aggregated", count)
	})

	c.Run("wait for process votes", func(c *qt.C) {
		// Check vote status and return whether all votes are processed
		checkVoteStatus := func() bool {
			txt := strings.Builder{}
			txt.WriteString("Vote status: ")
			allProcessed := true

			// Check status for each vote
			for i, voteID := range voteIDs {
				// Construct the status endpoint URL
				statusEndpoint := api.EndpointWithParam(
					api.EndpointWithParam(api.VoteStatusEndpoint,
						api.VoteStatusProcessIDParam, pid.String()),
					api.VoteStatusVoteIDParam, voteID.String())

				// Make the request to get the vote status
				body, statusCode, err := cli.Request("GET", nil, nil, statusEndpoint)
				c.Assert(err, qt.IsNil)
				c.Assert(statusCode, qt.Equals, 200)

				// Parse the response body to get the status
				var statusResponse api.VoteStatusResponse
				err = json.NewDecoder(bytes.NewReader(body)).Decode(&statusResponse)
				c.Assert(err, qt.IsNil)

				// Verify the status is valid
				c.Assert(statusResponse.Status, qt.Not(qt.Equals), "")

				// Check if the vote is processed
				if statusResponse.Status != "processed" {
					allProcessed = false
				}

				// Write to the string builder for logging
				txt.WriteString(fmt.Sprintf("#%d:%s ", i, statusResponse.Status))
			}

			// Log the vote status
			t.Log(txt.String())
			return allProcessed
		}

		// Set up timeout based on context deadline
		var timeoutCh <-chan time.Time
		deadline, hasDeadline := ctx.Deadline()

		if hasDeadline {
			// If context has a deadline, set timeout to 15 seconds before it
			// to allow for clean shutdown and error reporting
			remainingTime := time.Until(deadline)
			timeoutBuffer := 15 * time.Second

			// If we have less than the buffer time left, use half of the remaining time
			if remainingTime <= timeoutBuffer {
				timeoutBuffer = remainingTime / 2
			}

			effectiveTimeout := remainingTime - timeoutBuffer
			timeoutCh = time.After(effectiveTimeout)
			t.Logf("Test will timeout in %v (deadline: %v)", effectiveTimeout, deadline)
		} else {
			// No deadline set, use a reasonable default
			timeOut := 5 * time.Minute
			if os.Getenv("DEBUG") != "" && os.Getenv("DEBUG") != "false" {
				timeOut = 30 * time.Minute
			}
			timeoutCh = time.After(timeOut)
			t.Logf("No test deadline found, using %s minute default timeout", timeOut.String())
		}

		// Check vote status periodically
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if checkVoteStatus() {
					c.Logf("All %d votes reached 'processed' status", numBallots)
					// All votes are processed, finalize the process
					t.Log("Finalizing process...")
					services.Finalizer.OndemandCh <- pid
					t.Log("Waiting for finalization to complete...")
					results, err := services.Finalizer.WaitUntilFinalized(t.Context(), pid)
					c.Assert(err, qt.IsNil)
					c.Assert(results, qt.Not(qt.IsNil))
					c.Logf("Finalization results: %v", results)
					return
				}
			case <-timeoutCh:
				c.Fatalf("Timeout waiting for votes to be processed - all %d votes did not reach 'processed' status in time", numBallots)
			}
		}
	})
}
