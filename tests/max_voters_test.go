package tests

import (
	"context"
	"math/big"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/client"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/prover/debug"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	"github.com/vocdoni/davinci-node/types"
)

func TestMaxVoters(t *testing.T) {
	// Install log monitor that panics on Error level logs
	previousLogger := log.EnablePanicOnError(t.Name())
	defer log.RestoreLogger(previousLogger)

	// Create a global context to be used throughout the test
	globalCtx, globalCancel := context.WithTimeout(t.Context(), maxTestTimeout())
	defer globalCancel()

	initialVoters := 2
	totalVoters := initialVoters + 1 // one extra voter to test maxVoters limit
	c := qt.New(t)

	var (
		voteIDs []types.VoteID
		ks      []*big.Int
	)

	if isDebugTest() {
		prover.SetProver(debug.NewDebugProver(t))
	}

	processConfig := setupProcess(c, globalCtx, services.Contracts.ChainID, types.CensusOriginMerkleTreeOffchainStaticV1, totalVoters, initialVoters)
	encKey, err := services.SequencerClient.EncryptionKeys(processConfig.ProcessID)
	c.Assert(err, qt.IsNil)

	signers, err := processConfig.VotersConfig.Signers()
	c.Assert(err, qt.IsNil)

	votes := []api.Vote{}
	votersFieldsValues := [][]*types.BigInt{}
	c.Run("create votes", func(c *qt.C) {
		for _, signer := range signers[:initialVoters] {
			// Generate voter secret
			k, err := specutil.RandomK()
			c.Assert(err, qt.IsNil)
			ks = append(ks, k)
			// Generate random ballot fields and save them for results checks
			fields := client.RandomBallotFields(processConfig.BallotMode)
			votersFieldsValues = append(votersFieldsValues, fields)
			// Generate vote
			vote, err := services.SequencerClient.NewVote(processConfig, encKey, signer, k, fields, nil)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			votes = append(votes, vote)
		}
	})

	c.Run("send votes and wait for settled", func(c *qt.C) {
		// Submit the votes
		voteIDs, err = services.SequencerClient.SubmitVotes(votes...)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to submit vote"))
		c.Logf("%d votes sent, waiting for 'settled' status...", len(voteIDs))

		// Wait for settled status
		if err := client.WaitUntilCondition(globalCtx, 10*time.Second, func() bool {
			if allSettled, failed, err := services.SequencerClient.EnsureVotesStatus(processConfig.ProcessID, voteIDs, client.VoteIDStatusSettled); !allSettled {
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to check vote status"))
				if len(failed) > 0 {
					t.Fatalf("Some votes failed to be settled: %v", failed)
				}
			}

			votersCount, err := services.SequencerClient.OnchainProcessVotersCount(processConfig.ProcessID)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published votes from contract"))
			return votersCount == initialVoters
		}); err != nil {
			c.Fatalf("Timeout waiting for votes to be settled and published at contract")
			c.FailNow()
		}

		// Check that the number of voters is the expected
		votersCount, err := services.SequencerClient.OnchainProcessVotersCount(processConfig.ProcessID)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published votes from contract"))
		c.Assert(votersCount, qt.Equals, initialVoters)

		t.Log("All votes settled.")
	})

	c.Run("handle maxVoters reached", func(c *qt.C) {
		voteIDs = []types.VoteID{} // reset voteIDs slice to only store new vote

		extraSigner := signers[initialVoters] // get an extra signer from the created census
		// Generate a vote for the new participant
		randFields := client.RandomBallotFields(processConfig.BallotMode)
		vote, err := services.SequencerClient.NewVote(processConfig, encKey, extraSigner, nil, randFields, nil)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))

		c.Run("try to create a new vote even the maxVoters is reached", func(c *qt.C) {
			_, err := services.SequencerClient.SubmitVotes(vote)
			c.Assert(err, qt.IsNotNil, qt.Commentf("Expected error when submitting vote"))
			c.Assert(err.Error(), qt.Contains, api.ErrProcessMaxVotersReached.Error())
		})

		c.Run("update maxVoters", func(c *qt.C) {
			// Set the max voters to a higher number to allow new votes
			err = services.SequencerClient.UpdateMaxVoters(processConfig.ProcessID, totalVoters)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to update max voters"))

			if err := client.WaitUntilCondition(globalCtx, 10*time.Second, func() bool {
				// Get the process from storage
				process, err := services.Storage.Process(processConfig.ProcessID)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to get process from storage"))
				return process.MaxVoters.MathBigInt().Int64() == int64(totalVoters)
			}); err != nil {
				c.Fatalf("Timeout waiting for process state root to be updated")
				c.FailNow()
			}
			t.Logf("Process maxVoters updated.")
		})

		c.Run("update maxVoters and create a new vote", func(c *qt.C) {
			// Make the request to cast the vote again
			newVoteIDs, err := services.SequencerClient.SubmitVotes(vote)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to submit vote"))
			c.Assert(newVoteIDs, qt.HasLen, 1)

			// Save vote fields for results checks
			votersFieldsValues = append(votersFieldsValues, randFields)

			// Wait for settled status of extra votes
			if err := client.WaitUntilCondition(globalCtx, 10*time.Second, func() bool {
				if allSettled, failed, err := services.SequencerClient.EnsureVotesStatus(processConfig.ProcessID, newVoteIDs, client.VoteIDStatusSettled); !allSettled {
					c.Assert(err, qt.IsNil, qt.Commentf("Failed to check vote status"))
					if len(failed) > 0 {
						t.Fatalf("Some votes failed to be settled: %v", failed)
					}
				}

				votersCount, err := services.SequencerClient.OnchainProcessVotersCount(processConfig.ProcessID)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published votes from contract"))
				return votersCount == totalVoters
			}); err != nil {
				c.Fatalf("Timeout waiting for votes to be settled and published at contract")
				c.FailNow()
			}
			t.Log("All extra votes settled.")
		})
	})

	c.Run("finish process and wait for results", func(c *qt.C) {
		// Calculate expected results
		expectedResults := client.CalculateExpectedResults(votersFieldsValues)
		t.Logf("Expected results: %v", expectedResults)

		// Finish the process
		err := services.SequencerClient.StopProcess(processConfig.ProcessID)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to finish process on contract"))

		var results []*types.BigInt
		if err := client.WaitUntilCondition(globalCtx, 2*time.Second, func() bool {
			results, err = services.SequencerClient.OnchainProcessResults(processConfig.ProcessID)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published results from contract"))
			return results != nil
		}); err != nil {
			c.Fatalf("Timeout waiting for process to finish")
			c.FailNow()
		}
		t.Logf("Results published: %v", results)
		c.Assert(results, qt.DeepEquals, expectedResults)
	})
}
