package tests

import (
	"context"
	"math/big"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/prover/debug"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/tests/helpers"
	"github.com/vocdoni/davinci-node/types"
)

func TestMaxVoters(t *testing.T) {
	// Install log monitor that panics on Error level logs
	previousLogger := log.EnablePanicOnError(t.Name())
	defer log.RestoreLogger(previousLogger)

	// Create a global context to be used throughout the test
	globalCtx, globalCancel := context.WithTimeout(t.Context(), helpers.MaxTestTimeout(t))
	defer globalCancel()

	initialVoters := 2
	totalVoters := initialVoters + 1 // one extra voter to test maxVoters limit
	c := qt.New(t)

	var (
		err           error
		pid           types.ProcessID
		stateRoot     types.HexBytes
		encryptionKey *types.EncryptionKey
		signers       []*ethereum.Signer
		censusRoot    []byte
		censusURI     string
		// Store the voteIDs returned from the API to check their status later
		voteIDs []types.VoteID
		ks      []*big.Int
	)

	if helpers.IsDebugTest() {
		services.Sequencer.SetProver(debug.NewDebugProver(t))
	}

	c.Run("create process", func(c *qt.C) {
		censusCtx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Create census with numVoters participants
		censusRoot, censusURI, signers, err = helpers.NewCensusWithRandomVoters(censusCtx, types.CensusOriginMerkleTreeOffchainStaticV1, totalVoters)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create census"))
		c.Assert(len(signers), qt.Equals, totalVoters)

		// create process in the sequencer
		pid, encryptionKey, err = helpers.NewProcess(services.Contracts, services.HTTPClient)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))

		// now create process in contracts with initialVoters as maxVoters
		onchainPID, err := helpers.NewProcessOnChain(services.Contracts, types.CensusOriginMerkleTreeOffchainStaticV1, censusURI, censusRoot, defaultBallotMode, encryptionKey, initialVoters)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in contracts"))
		c.Assert(onchainPID.String(), qt.Equals, pid.String())

		if err := helpers.WaitUntilCondition(globalCtx, time.Millisecond*200, func() bool {
			if process, err := services.Storage.Process(pid); err == nil {
				stateRoot = process.StateRoot.Bytes()
				return true
			}
			return false
		}); err != nil {
			c.Fatal("Timeout waiting for process to be created in storage")
			c.FailNow()
		}
		t.Logf("Process ID: %s", pid.String())

		// Wait for the process to be registered in the sequencer
		if err := helpers.WaitUntilCondition(globalCtx, time.Millisecond*200, func() bool {
			return services.Sequencer.ExistsProcessID(pid)
		}); err != nil {
			c.Fatal("Timeout waiting for process to be registered in sequencer")
			c.FailNow()
		}
	})

	c.Run("create votes", func(c *qt.C) {
		for i, signer := range signers[:initialVoters] {
			// generate a vote for the first participant
			k, err := specutil.RandomK()
			c.Assert(err, qt.IsNil)
			vote, err := helpers.NewVoteWithRandomFields(pid, defaultBallotMode, encryptionKey, signer, k)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			// generate census proof
			vote.CensusProof, err = helpers.CreateCensusProof(types.CensusOriginMerkleTreeOffchainStaticV1, pid, signers[i].Address())
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))
			// Make the request to cast the vote
			_, status, err := services.HTTPClient.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, 200)

			// Save the voteID for status checks
			voteIDs = append(voteIDs, vote.VoteID)
			ks = append(ks, k)
		}
	})

	c.Run("wait for settled votes", func(c *qt.C) {
		t.Logf("Waiting for %d votes to be settled", initialVoters)
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
			return votersCount == initialVoters
		}); err != nil {
			c.Fatalf("Timeout waiting for votes to be settled and published at contract")
			c.FailNow()
		}
		t.Log("All votes settled.")
	})

	c.Run("wait until the stateroot is updated", func(c *qt.C) {
		if err := helpers.WaitUntilCondition(globalCtx, 10*time.Second, func() bool {
			// Get the process from storage
			process, err := services.Storage.Process(pid)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to get process from storage"))
			return process.StateRoot.String() != stateRoot.String()
		}); err != nil {
			c.Fatalf("Timeout waiting for process state root to be updated")
			c.FailNow()
		}
		t.Logf("Process state root updated.")
	})

	c.Run("handle maxVoters reached", func(c *qt.C) {
		voteIDs = []types.VoteID{} // reset voteIDs slice to only store new vote

		extraSigner := signers[initialVoters] // get an extra signer from the created census
		// generate a vote for the new participant
		vote, err := helpers.NewVoteWithRandomFields(pid, defaultBallotMode, encryptionKey, extraSigner, nil)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
		// generate census proof for the participant
		vote.CensusProof, err = helpers.CreateCensusProof(types.CensusOriginMerkleTreeOffchainStaticV1, pid, extraSigner.Address())
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))

		c.Run("try to create a new vote even the maxVoters is reached", func(c *qt.C) {
			// Make the request to cast the vote
			body, status, err := services.HTTPClient.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, api.ErrProcessMaxVotersReached.HTTPstatus)
			c.Assert(string(body), qt.Contains, api.ErrProcessMaxVotersReached.Error())
		})

		c.Run("update maxVoters", func(c *qt.C) {
			// Set the max voters to a higher number to allow new votes
			err = helpers.UpdateMaxVotersOnChain(services.Contracts, pid, totalVoters)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to update max voters"))

			if err := helpers.WaitUntilCondition(globalCtx, 10*time.Second, func() bool {
				// Get the process from storage
				process, err := services.Storage.Process(pid)
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
			_, status, err := services.HTTPClient.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, 200)

			// append the new vote stuff to the lists for later checks
			voteIDs = append(voteIDs, vote.VoteID)
		})
	})

	c.Run("wait for settled extra votes", func(c *qt.C) {
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
			return votersCount == totalVoters
		}); err != nil {
			c.Fatalf("Timeout waiting for votes to be settled and published at contract")
			c.FailNow()
		}
		t.Log("All extra votes settled.")
	})

	c.Run("finish process and wait for results", func(c *qt.C) {
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
	})
}
