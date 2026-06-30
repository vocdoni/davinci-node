package tests

import (
	"context"
	"fmt"
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
		if err := client.WaitUntilCondition(globalCtx, 10*time.Second, func() (bool, error) {
			allSettled, failed, err := services.SequencerClient.EnsureVotesStatus(processConfig.ProcessID, voteIDs, client.VoteIDStatusSettled)
			if err != nil {
				return false, err
			}
			if !allSettled && len(failed) > 0 {
				return false, fmt.Errorf("at least %d votes failed: %v", len(failed), failed)
			}

			votersCount, err := services.SequencerClient.OnchainProcessVotersCount(processConfig.ProcessID)
			return votersCount == initialVoters, err
		}); err != nil {
			c.Fatalf("Error waiting for votes to be settled: %v", err)
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

			if err := client.WaitUntilCondition(globalCtx, 10*time.Second, func() (bool, error) {
				// Get the process from storage
				process, err := services.Storage.Process(processConfig.ProcessID)
				if err != nil {
					return false, err
				}
				return process.MaxVoters.MathBigInt().Int64() == int64(totalVoters), nil
			}); err != nil {
				c.Fatalf("Error waiting for maxVoters to be updated: %v", err)
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
			if err := client.WaitUntilCondition(globalCtx, 10*time.Second, func() (bool, error) {
				allSettled, failed, err := services.SequencerClient.EnsureVotesStatus(processConfig.ProcessID, newVoteIDs, client.VoteIDStatusSettled)
				if err != nil {
					return false, err
				}
				if !allSettled && len(failed) > 0 {
					return false, fmt.Errorf("at least %d votes failed: %v", len(failed), failed)
				}

				votersCount, err := services.SequencerClient.OnchainProcessVotersCount(processConfig.ProcessID)
				return votersCount == totalVoters, err
			}); err != nil {
				c.Fatalf("Error waiting for votes to be settled: %v", err)
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
		if err := client.WaitUntilCondition(globalCtx, 2*time.Second, func() (bool, error) {
			results, err = services.SequencerClient.OnchainProcessResults(processConfig.ProcessID)
			return results != nil, err
		}); err != nil {
			c.Fatalf("Error waiting for results: %v", err)
			c.FailNow()
		}
		t.Logf("Results published: %v", results)
		c.Assert(results, qt.DeepEquals, expectedResults)
	})
}
