package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/client"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/prover/debug"
	"github.com/vocdoni/davinci-node/types"
)

func TestOverwriteVotes(t *testing.T) {
	// Install log monitor that panics on Error level logs
	previousLogger := log.EnablePanicOnError(t.Name())
	defer log.RestoreLogger(previousLogger)

	// Create a global context to be used throughout the test
	globalCtx, globalCancel := context.WithTimeout(t.Context(), maxTestTimeout())
	defer globalCancel()

	numVoters := 2
	c := qt.New(t)

	var voteIDs []types.VoteID

	if isDebugTest() {
		prover.SetProver(debug.NewDebugProver(t))
	}

	processConfig := setupProcess(c, globalCtx, services.Contracts.ChainID, types.CensusOriginMerkleTreeOffchainStaticV1, numVoters, numVoters)

	encKey, err := services.SequencerClient.EncryptionKeys(processConfig.ProcessID)
	c.Assert(err, qt.IsNil)

	signers, err := processConfig.VotersConfig.Signers()
	c.Assert(err, qt.IsNil)

	votes := []api.Vote{}
	c.Run("create votes", func(c *qt.C) {
		for _, signer := range signers {
			// Generate vote
			fields := client.RandomBallotFields(processConfig.BallotMode)
			vote, err := services.SequencerClient.NewVote(processConfig, encKey, signer, nil, fields, nil)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			votes = append(votes, vote)
		}
		c.Assert(votes, qt.HasLen, numVoters)
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
			return votersCount == numVoters, err
		}); err != nil {
			c.Fatalf("Error waiting for votes to be settled: %v", err)
			c.FailNow()
		}

		// Check that the number of voters is the expected
		votersCount, err := services.SequencerClient.OnchainProcessVotersCount(processConfig.ProcessID)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published votes from contract"))
		c.Assert(votersCount, qt.Equals, numVoters)

		t.Log("All votes settled.")
	})

	votersFieldsValues := [][]*types.BigInt{}
	c.Run("overwrite valid votes", func(c *qt.C) {
		// Clear votes
		votes = []api.Vote{}
		for _, signer := range signers {
			// Generate random ballot fields and save them for results checks
			fields := client.RandomBallotFields(processConfig.BallotMode)
			votersFieldsValues = append(votersFieldsValues, fields)
			// Generate vote
			vote, err := services.SequencerClient.NewVote(processConfig, encKey, signer, nil, fields, nil)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			votes = append(votes, vote)
		}
		c.Assert(votes, qt.HasLen, numVoters)
	})

	c.Run("submit overwrite votes", func(c *qt.C) {
		// Submit the votes
		voteIDs, err = services.SequencerClient.SubmitVotes(votes...)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to submit vote"))
		c.Logf("%d overwrite votes sent, waiting for 'settled' status...", len(voteIDs))

		// Wait for settled status
		if err := client.WaitUntilCondition(globalCtx, 10*time.Second, func() (bool, error) {
			allSettled, failed, err := services.SequencerClient.EnsureVotesStatus(processConfig.ProcessID, voteIDs, client.VoteIDStatusSettled)
			if err != nil {
				return false, err
			}
			if !allSettled && len(failed) > 0 {
				return false, fmt.Errorf("at least %d votes failed: %v", len(failed), failed)
			}

			votersCount, err := services.SequencerClient.OnchainProcessOverwrittenVotesCount(processConfig.ProcessID)
			return votersCount == numVoters, err
		}); err != nil {
			c.Fatalf("Error waiting for votes to be settled: %v", err)
			c.FailNow()
		}
		t.Log("All overwritten votes settled.")
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
