package tests

import (
	"context"
	"math/big"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/prover/debug"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/tests/helpers"
	"github.com/vocdoni/davinci-node/types"
)

func TestCSPCensus(t *testing.T) {
	// Install log monitor that panics on Error level logs
	previousLogger := log.EnablePanicOnError(t.Name())
	defer log.RestoreLogger(previousLogger)

	numVoters := 2

	// Create a global context to be used throughout the test
	globalCtx, globalCancel := context.WithTimeout(t.Context(), helpers.MaxTestTimeout(t))
	defer globalCancel()

	c := qt.New(t)

	var (
		voteIDs []types.VoteID
		ks      []*big.Int
	)

	if helpers.IsDebugTest() {
		prover.SetProver(debug.NewDebugProver(t))
	}

	setup := setupProcess(c, t, globalCtx, types.CensusOriginCSPEdDSABabyJubJubV1, numVoters, numVoters)
	pid, encryptionKey, signers := setup.pid, setup.encryptionKey, setup.signers

	votersFieldsValues := [][]*types.BigInt{}
	c.Run("create votes", func(c *qt.C) {
		for i, signer := range signers {
			// generate a vote for the first participant
			k, err := specutil.RandomK()
			c.Assert(err, qt.IsNil)
			vote, randFields, err := helpers.NewVoteWithRandomFields(pid, defaultBallotMode, encryptionKey, signer, k)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			// generate census proof
			vote.CensusProof, err = helpers.CreateCensusProof(types.CensusOriginCSPEdDSABabyJubJubV1, pid, signers[i].Address())
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))
			// Make the request to cast the vote
			_, status, err := services.HTTPClient.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, 200)

			// Save the voteID for status checks
			voteIDs = append(voteIDs, vote.VoteID)
			ks = append(ks, k)
			// Save vote fields for results checks
			votersFieldsValues = append(votersFieldsValues, randFields)
		}
		c.Assert(ks, qt.HasLen, numVoters)
		c.Assert(voteIDs, qt.HasLen, numVoters)
	})

	c.Run("wait for settled votes", func(c *qt.C) {
		t.Logf("Waiting for %d votes to be settled", numVoters)
		if err := helpers.WaitUntilCondition(globalCtx, 10*time.Second, func() bool {
			// Check that votes are settled (state transitions confirmed on blockchain)
			if allSettled, failed, err := helpers.EnsureVotesStatus(services.HTTPClient, pid, voteIDs, storage.VoteIDStatusName(storage.VoteIDStatusSettled)); !allSettled {
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to check vote status"))
				if len(failed) > 0 {
					hexFailed := types.SliceOf(failed, func(v types.VoteID) string { return v.String() })
					t.Fatalf("Some votes failed to be settled: %v", hexFailed)
				}
			}
			votersCount, err := helpers.FetchProcessVotersCountOnChain(services.Contracts, pid)
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
		expectedResults := helpers.CalculateExpectedResults(votersFieldsValues)
		t.Logf("Expected results: %v", expectedResults)

		err := helpers.FinishProcessOnChain(services.Contracts, pid)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to finish process on contract"))
		results, err := services.Sequencer.WaitUntilResults(t.Context(), pid)
		c.Assert(err, qt.IsNil)
		c.Logf("Results calculated: %v, waiting for onchain results...", results)

		var pubResults []*types.BigInt
		if err := helpers.WaitUntilCondition(globalCtx, 10*time.Second, func() bool {
			pubResults, err = helpers.FetchResultsOnChain(services.Contracts, pid)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published results from contract"))
			return pubResults != nil
		}); err != nil {
			c.Fatalf("Timeout waiting for votes to be processed and published at contract")
			c.FailNow()
		}
		t.Logf("Results published: %v", pubResults)
		c.Assert(pubResults, qt.DeepEquals, expectedResults)
	})
}
