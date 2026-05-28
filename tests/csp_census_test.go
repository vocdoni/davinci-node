package tests

import (
	"context"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/client"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/prover/debug"
	"github.com/vocdoni/davinci-node/types"
)

func TestCSPCensus(t *testing.T) {
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

	processConfig := setupProcess(c, globalCtx, services.Contracts.ChainID, types.CensusOriginCSPEdDSABabyJubJubV1, numVoters, numVoters)

	var voteIDs []types.VoteID
	votersFieldsValues := [][]*types.BigInt{}
	c.Run("create votes", func(c *qt.C) {
		votes, fields, err := services.SequencerClient.CreateRandomVotes(processConfig)
		c.Assert(err, qt.IsNil, qt.Commentf("error creating random votes"))

		voteIDs, err = services.SequencerClient.SubmitVotes(votes...)
		c.Assert(err, qt.IsNil, qt.Commentf("error submitting votes"))

		c.Assert(voteIDs, qt.HasLen, numVoters)
		votersFieldsValues = append(votersFieldsValues, fields...)
	})

	c.Run("wait for settled votes", func(c *qt.C) {
		t.Logf("Waiting for %d votes to be settled", numVoters)
		if err := client.WaitUntilCondition(globalCtx, 10*time.Second, func() bool {
			if allSettled, failed, err := services.SequencerClient.EnsureVotesStatus(processConfig.ProcessID, voteIDs, client.VoteIDStatusSettled); !allSettled {
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to check vote status"))
				if len(failed) > 0 {
					t.Fatalf("Some votes failed to be settled: %v", failed)
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
}
