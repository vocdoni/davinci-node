package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/sequencer"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
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
	services.Sequencer.SetBatchTimeWindow(time.Second * 120)

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
			vote, _, _ := createVote(c, pid, ballotMode, encryptionKey, signers[i], nil, nil)
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
			c.Logf("Vote %d (addr: %s - k: %s) created with ID: %s", i, vote.Address.String(), k.String(), voteResponse.VoteID.String())

			// Save the voteID for status checks
			voteIDs = append(voteIDs, voteResponse.VoteID)
			count++
			// try to send the vote again to check for duplicate handling
			body, status, err = cli.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, 400)
			c.Assert(string(body), qt.Contains, api.ErrBallotAlreadySubmitted.Err.Error())
		}
		c.Assert(count, qt.Equals, numBallots)
		// Wait for the vote to be registered
		t.Logf("Waiting for %d votes to be registered and aggregated", count)
	})

	c.Run("create invalid votes", func(c *qt.C) {
		vote := createVoteFromInvalidVoter(c, pid, ballotMode, encryptionKey)
		// Make the request to try cast the vote
		body, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, 400)
		c.Assert(string(body), qt.Contains, api.ErrMalformedBody.Withf("invalid census proof").Error())
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
			timeOut := 10 * time.Minute
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
				// Check that votes are settled (state transitions confirmed on blockchain)
				if allSettled, failed := checkVoteStatus(t, cli, pid, voteIDs, storage.VoteIDStatusName(storage.VoteIDStatusProcessed)); !allSettled {
					if len(failed) > 0 {
						hexFailed := make([]string, len(failed))
						for i, v := range failed {
							hexFailed[i] = v.String()
						}
						t.Fatalf("Some votes failed to be processed: %v", hexFailed)
					}
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
		results, err := services.Sequencer.WaitUntilResults(t.Context(), pid)
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

				// Check that all vote IDs are now marked as "settled"
				t.Log("Checking that all vote IDs are marked as settled...")
				if allSettled, failedSettled := checkVoteStatus(t, cli, pid, voteIDs, storage.VoteIDStatusName(storage.VoteIDStatusSettled)); !allSettled {
					if len(failedSettled) > 0 {
						hexFailedSettled := make([]string, len(failedSettled))
						for i, v := range failedSettled {
							hexFailedSettled[i] = v.String()
						}
						t.Fatalf("Some votes are not yet settled: %v, waiting...", hexFailedSettled)
					}
					t.Log("Not all votes are settled yet, waiting...")
					continue
				}
				t.Log("All votes are now marked as settled!")
				return
			case <-timeoutCh:
				c.Fatalf("Timeout waiting for votes to be processed and published at contract")
			}
		}
	})
}
