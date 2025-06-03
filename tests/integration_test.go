package tests

import (
	"bytes"
	"encoding/json"
	"os"
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
	ctx := t.Context()
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
		// Set up timeout based on context deadline
		var timeoutCh <-chan time.Time
		deadline, hasDeadline := t.Deadline()

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

	ResultsLoop:
		for {
			select {
			case <-ticker.C:
				if allProcessed, failed := checkProcessedVotes(t, cli, pid, voteIDs); !allProcessed {
					continue
				} else if len(failed) > 0 {
					hexFailed := make([]string, len(failed))
					for i, v := range failed {
						hexFailed[i] = v.String()
					}
					t.Fatalf("Some votes failed to process: %v", hexFailed)
				}
				if publishedVotes(t, services.Contracts, pid) < numBallots {
					continue
				}
				break ResultsLoop
			case <-timeoutCh:
				c.Fatalf("Timeout waiting for votes to be processed and published at contract")
			}
		}

		t.Log("All votes published, finalizing process...")
		finishProcessOnContract(t, services.Contracts, pid)
		results, err := services.Sequencer.WaitUntilFinalized(t.Context(), pid)
		c.Assert(err, qt.IsNil)
		c.Logf("Results calculated: %v, waiting for onchain results...", results)

		for {
			select {
			case <-ticker.C:
				results := publishedResults(t, services.Contracts, pid)
				if results == nil {
					t.Log("Results not yet published, waiting...")
					continue
				}
				t.Logf("Results published: %v", results)
				return
			case <-timeoutCh:
				c.Fatalf("Timeout waiting for votes to be processed and published at contract")
			}
		}
	})
}
