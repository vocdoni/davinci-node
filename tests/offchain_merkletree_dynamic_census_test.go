package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/client"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/prover/debug"
	"github.com/vocdoni/davinci-node/types"
)

func TestOffChainMerkleTreeDynamicCensus(t *testing.T) {
	// Install log monitor that panics on Error level logs
	previousLogger := log.EnablePanicOnError(t.Name())
	defer log.RestoreLogger(previousLogger)

	numVoters := 2

	// Create a global context to be used throughout the test
	globalCtx, globalCancel := context.WithTimeout(t.Context(), maxTestTimeout())
	defer globalCancel()

	c := qt.New(t)

	if isDebugTest() {
		prover.SetProver(debug.NewDebugProver(t))
	}

	processConfig := setupProcess(c, globalCtx, services.Contracts.ChainID, types.CensusOriginMerkleTreeOffchainDynamicV1, numVoters, numVoters*2)
	encKey, err := services.SequencerClient.EncryptionKeys(processConfig.ProcessID)
	c.Assert(err, qt.IsNil)

	signers, err := processConfig.VotersConfig.Signers()
	c.Assert(err, qt.IsNil)

	var voteIDs []types.VoteID
	votes := []api.Vote{}
	votersFieldsValues := [][]*types.BigInt{}
	c.Run("create votes", func(c *qt.C) {
		for _, signer := range signers {
			// Generate random ballot fields and save them for results checks
			fields := client.RandomBallotFields(processConfig.BallotMode)
			votersFieldsValues = append(votersFieldsValues, fields)
			// Generate vote
			vote, err := services.SequencerClient.NewVote(processConfig, encKey, signer, nil, fields, nil)
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

	c.Run("create new census", func(c *qt.C) {
		newCensusProcessConfig := &client.ProcessConfig{
			ProcessID:    processConfig.ProcessID,
			BallotMode:   processConfig.BallotMode,
			VotersConfig: processConfig.VotersConfig,
			CensusConfig: client.CensusConfig{
				CensusOrigin: types.CensusOriginMerkleTreeOffchainDynamicV1.String(),
			},
		}

		newVotersConfig := client.VotersConfig{
			NumVoters:     numVoters,
			DefaultWeight: types.NewInt(testutil.Weight),
		}
		newVotersConfig.VotersInfo, err = newVotersConfig.RandomVotersInfo()
		c.Assert(err, qt.IsNil)
		newCensusProcessConfig.VotersConfig.VotersInfo = append(newCensusProcessConfig.VotersConfig.VotersInfo, newVotersConfig.VotersInfo...)

		newCensus, err := services.SequencerClient.CreateCensus(newCensusProcessConfig)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create new census"))

		newSigners, err := newVotersConfig.Signers()
		c.Assert(err, qt.IsNil)

		c.Run("try to vote with a non voter", func(c *qt.C) {
			fields := client.RandomBallotFields(processConfig.BallotMode)
			vote, err := services.SequencerClient.NewVote(newCensusProcessConfig, encKey, newSigners[0], nil, fields, types.NewInt(1))
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))

			_, err = services.SequencerClient.SubmitVotes(vote)
			c.Assert(err, qt.IsNotNil)
			c.Assert(err.Error(), qt.Contains, api.ErrInvalidCensusProof.Error())
		})

		c.Run("update process census", func(c *qt.C) {
			err := services.SequencerClient.UpdateCensus(processConfig.ProcessID, *newCensus)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to update process census"))

			if err := client.WaitUntilCondition(globalCtx, 2*time.Second, func() (bool, error) {
				process, err := services.Storage.Process(processConfig.ProcessID)
				return process.Census.CensusRoot.Equal(newCensus.CensusRoot), err
			}); err != nil {
				c.Fatalf("Timeout waiting for census to be updated")
				c.FailNow()
			}
		})

		c.Run("send new voters votes", func(c *qt.C) {
			votes := []api.Vote{}
			for _, signer := range newSigners {
				// Generate random ballot fields and save them for results checks
				fields := client.RandomBallotFields(processConfig.BallotMode)
				votersFieldsValues = append(votersFieldsValues, fields)
				// Generate vote
				vote, err := services.SequencerClient.NewVote(newCensusProcessConfig, encKey, signer, nil, fields, nil)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
				votes = append(votes, vote)
			}

			// Submit the votes
			voteIDs, err = services.SequencerClient.SubmitVotes(votes...)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to submit vote"))
			c.Logf("%d votes sent, waiting for 'settled' status...", len(voteIDs))
		})

		c.Run("wait for settled new votes", func(c *qt.C) {
			t.Logf("Waiting for %d votes to be settled", numVoters)
			if err := client.WaitUntilCondition(globalCtx, 10*time.Second, func() (bool, error) {
				allSettled, failed, err := services.SequencerClient.EnsureVotesStatus(processConfig.ProcessID, voteIDs, client.VoteIDStatusSettled)
				if err != nil {
					return false, err
				}
				if !allSettled && len(failed) > 0 {
					return false, fmt.Errorf("at least %d votes failed: %v", len(failed), failed)
				}

				votersCount, err := services.SequencerClient.OnchainProcessVotersCount(processConfig.ProcessID)
				return votersCount == numVoters*2, err
			}); err != nil {
				c.Fatalf("Error waiting for votes to be settled: %v", err)
				c.FailNow()
			}
			t.Log("All votes settled.")
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
