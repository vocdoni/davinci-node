package tests

import (
	"context"
	"math/big"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/client"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/prover/debug"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	"github.com/vocdoni/davinci-node/types"
)

func TestVoteCastingRejections(t *testing.T) {
	// Install log monitor that panics on Error level logs
	previousLogger := log.EnablePanicOnError(t.Name())
	defer log.RestoreLogger(previousLogger)

	// Create a global context to be used throughout the test
	globalCtx, globalCancel := context.WithTimeout(t.Context(), maxTestTimeout())
	defer globalCancel()

	numVoters := 2
	c := qt.New(t)

	var (
		voteIDs []types.VoteID
		ks      []*big.Int
	)

	if isDebugTest() {
		prover.SetProver(debug.NewDebugProver(t))
	}

	processConfig := setupProcess(c, globalCtx, services.Contracts.ChainID, types.CensusOriginMerkleTreeOffchainStaticV1, numVoters, numVoters)
	encKey, err := services.SequencerClient.EncryptionKeys(processConfig.ProcessID)
	c.Assert(err, qt.IsNil)

	signers, err := processConfig.VotersConfig.Signers()
	c.Assert(err, qt.IsNil)

	c.Run("create invalid votes", func(c *qt.C) {
		fields := client.RandomBallotFields(processConfig.BallotMode)
		uknownVoter, err := ethereum.NewSigner()
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create ethereum signer"))
		vote, err := services.SequencerClient.NewVote(processConfig, encKey, uknownVoter, nil, fields, types.NewInt(1))
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote from invalid voter"))
		// Make the request to try cast the vote
		_, err = services.SequencerClient.SubmitVotes(vote)
		c.Assert(err, qt.IsNotNil)
		c.Assert(err.Error(), qt.Contains, api.ErrInvalidCensusProof.Error())
	})

	votersFieldsValues := [][]*types.BigInt{}
	c.Run("create votes", func(c *qt.C) {
		votes := []api.Vote{}
		for _, signer := range signers {
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
		c.Assert(ks, qt.HasLen, numVoters)
		c.Assert(votes, qt.HasLen, numVoters)

		voteIDs, err = services.SequencerClient.SubmitVotes(votes...)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to submit votes"))
	})

	c.Run("try to overwrite valid votes", func(c *qt.C) {
		for i, signer := range signers {
			// Generate new vote to try to overwrite the valid one
			fields := client.RandomBallotFields(processConfig.BallotMode)
			vote, err := services.SequencerClient.NewVote(processConfig, encKey, signer, ks[i], fields, nil)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))

			// Make the request to try cast the vote
			_, err = services.SequencerClient.SubmitVotes(vote)
			c.Assert(err, qt.IsNotNil)
			c.Assert(err.Error(), qt.Contains, api.ErrBallotAlreadyProcessing.Error())
		}
	})

	c.Run("wait for settled votes", func(c *qt.C) {
		t.Logf("Waiting for %d votes to be settled", numVoters)
		if err := client.WaitUntilCondition(globalCtx, 10*time.Second, func() bool {
			// Check that votes are settled (state transitions confirmed on blockchain)
			if allSettled, failed, err := services.SequencerClient.EnsureVotesStatus(processConfig.ProcessID, voteIDs, client.VoteIDStatusSettled); !allSettled {
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to check vote status"))
				if len(failed) > 0 {
					hexFailed := types.SliceOf(failed, func(v types.VoteID) string { return v.String() })
					t.Fatalf("Some votes failed to be settled: %v", hexFailed)
				}
			}
			votersCount, err := services.SequencerClient.OnchainProcessVotersCount(processConfig.ProcessID)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published votes from contract"))
			return votersCount == numVoters
		}); err != nil {
			c.Fatalf("Timeout waiting for votes to be settled and published at contract")
			c.FailNow()
		}
		t.Log("All votes settled.")
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

	c.Run("try to send votes to ended process", func(c *qt.C) {
		for i, signer := range signers {
			// generate a vote for the first participant
			fields := client.RandomBallotFields(processConfig.BallotMode)
			vote, err := services.SequencerClient.NewVote(processConfig, encKey, signer, ks[i], fields, nil)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			// Make the request to cast the vote
			_, err = services.SequencerClient.SubmitVotes(vote)
			c.Assert(err, qt.IsNotNil)
			c.Assert(err.Error(), qt.Contains, api.ErrProcessNotAcceptingVotes.Error())
		}
	})
}
