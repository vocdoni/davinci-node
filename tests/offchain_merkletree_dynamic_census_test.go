package tests

import (
	"bytes"
	"context"
	"math/big"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/prover/debug"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/tests/helpers"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

func TestOffChainMerkleTreeDynamicCensus(t *testing.T) {
	// Install log monitor that panics on Error level logs
	previousLogger := log.EnablePanicOnError(t.Name())
	defer log.RestoreLogger(previousLogger)

	numVoters := 2
	c := qt.New(t)

	var (
		err           error
		pid           *types.ProcessID
		encryptionKey *types.EncryptionKey
		signers       []*ethereum.Signer
		censusRoot    []byte
		censusURI     string
		// Store the voteIDs returned from the API to check their status later
		voteIDs []types.HexBytes
		ks      []*big.Int
	)

	if helpers.IsDebugTest() {
		services.Sequencer.SetProver(debug.NewDebugProver(t))
	}

	timeoutCh := helpers.TestTimeoutChan(t)

	c.Run("create process", func(c *qt.C) {
		censusCtx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Create census with numVoters participants
		censusRoot, censusURI, signers, err = helpers.TestCensusWithRandomVoters(censusCtx, types.CensusOriginMerkleTreeOffchainDynamicV1, numVoters)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create census"))
		c.Assert(len(signers), qt.Equals, numVoters)

		// create process in sequencer
		var stateRoot *types.HexBytes
		pid, encryptionKey, stateRoot, err = helpers.TestNewProcess(services.Contracts, services.HTTPClient, types.CensusOriginMerkleTreeOffchainDynamicV1, censusURI, censusRoot, defaultBallotMode)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))

		// now create process in contracts
		onchainPID, err := helpers.TestProcessOnChain(services.Contracts, types.CensusOriginMerkleTreeOffchainDynamicV1, censusURI, censusRoot, defaultBallotMode, encryptionKey, stateRoot, numVoters)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in contracts"))
		c.Assert(onchainPID.String(), qt.Equals, pid.String())

		if err := helpers.TestWaitForWithChannel(timeoutCh, time.Millisecond*200, func() bool {
			_, err := services.Storage.Process(pid)
			return err == nil
		}); err != nil {
			c.Fatal("Timeout waiting for process to be created and registered")
			c.FailNow()
		}
		t.Logf("Process ID: %s", pid.String())

		// Wait for the process to be registered in the sequencer
		if err := helpers.TestWaitForWithChannel(timeoutCh, time.Millisecond*200, func() bool {
			return services.Sequencer.ExistsProcessID(pid.Marshal())
		}); err != nil {
			c.Fatal("Timeout waiting for process to be registered in sequencer")
			c.FailNow()
		}
	})

	c.Run("create votes", func(c *qt.C) {
		for i, signer := range signers {
			// generate a vote for the first participant
			k := util.RandomBigInt(big.NewInt(100000000), big.NewInt(9999999999999999))
			vote, err := helpers.TestNewVoteWithRandomFields(pid, defaultBallotMode, encryptionKey, signer, k)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			// generate census proof
			vote.CensusProof, err = helpers.TestCensusProof(types.CensusOriginMerkleTreeOffchainDynamicV1, pid.Marshal(), signers[i].Address().Bytes())
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))
			// Make the request to cast the vote
			_, status, err := services.HTTPClient.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, 200)

			// Save the voteID for status checks
			voteIDs = append(voteIDs, vote.VoteID)
			ks = append(ks, k)
		}
		c.Assert(ks, qt.HasLen, numVoters)
		c.Assert(voteIDs, qt.HasLen, numVoters)
	})

	c.Run("test dynamic census", func(c *qt.C) {
		// create a signer that is not in the census
		signer, err := ethereum.NewSigner()
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create ethereum signer"))
		// try to vote with the new signer, should fail
		k := util.RandomBigInt(big.NewInt(100000000), big.NewInt(9999999999999999))
		vote, err := helpers.TestNewVoteWithRandomFields(pid, defaultBallotMode, encryptionKey, signer, k)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
		// generate census proof
		vote.CensusProof, err = helpers.TestCensusProof(types.CensusOriginMerkleTreeOffchainDynamicV1, pid.Marshal(), signer.Address().Bytes())
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))

		c.Run("try to vote with a non-census voter", func(c *qt.C) {
			// Make the request to cast the vote
			body, status, err := services.HTTPClient.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, api.ErrInvalidCensusProof.HTTPstatus)
			c.Assert(string(body), qt.Contains, api.ErrInvalidCensusProof.Error())
		})

		c.Run("update census", func(c *qt.C) {
			censusCtx, cancel := context.WithCancel(t.Context())
			defer cancel()

			// create a new census including the new signer
			signers = append(signers, signer)
			censusRoot, censusURI, _, err = helpers.TestCensusWithVoters(censusCtx, types.CensusOriginMerkleTreeOffchainDynamicV1, signers...)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create census"))

			// update the census in the contracts
			err = helpers.TestUpdateCensusOnChain(services.Contracts, pid, types.Census{
				CensusOrigin: helpers.TestCensusOrigin(),
				CensusRoot:   censusRoot,
				CensusURI:    censusURI,
			})
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to update process census in contracts"))
			// wait to new census in the sequencer
			if err := helpers.TestWaitForWithChannel(timeoutCh, time.Second*10, func() bool {
				process, err := services.Storage.Process(pid)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to get process from storage"))
				return bytes.Equal(process.Census.CensusRoot, censusRoot)
			}); err != nil {
				c.Fatal("Timeout waiting for process census to be updated in sequencer")
				c.FailNow()
			}
			t.Log("Process census root updated")
		})

		c.Run("vote with the new census voter", func(c *qt.C) {
			// Make the request to cast the vote
			_, status, err := services.HTTPClient.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, 200)

			// Save the voteID for status checks
			voteIDs = append(voteIDs, vote.VoteID)
			ks = append(ks, k)
		})
	})

	c.Run("wait for settled votes", func(c *qt.C) {
		t.Logf("Waiting for %d votes to be registered and aggregated", numVoters)
		if err := helpers.TestWaitForWithChannel(timeoutCh, time.Second*5, func() bool {
			// Check that votes are settled (state transitions confirmed on blockchain)
			if allSettled, failed, err := helpers.TestEnsureVotesStatus(services.HTTPClient, pid, voteIDs, storage.VoteIDStatusName(storage.VoteIDStatusSettled)); !allSettled {
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to check vote status"))
				if len(failed) > 0 {
					hexFailed := make([]string, len(failed))
					for i, v := range failed {
						hexFailed[i] = v.String()
					}
					t.Fatalf("Some votes failed to be settled: %v", hexFailed)
				}
			}
			votersCount, err := helpers.TestProcessVotersCountOnChain(services.Contracts, pid)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published votes from contract"))
			return votersCount == len(voteIDs)
		}); err != nil {
			c.Fatalf("Timeout waiting for votes to be registered and aggregated")
			c.FailNow()
		}
		t.Log("All votes settled.")
	})

	c.Run("finish process and wait for results", func(c *qt.C) {
		err := helpers.TestFinishProcessOnChain(services.Contracts, pid)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to finish process on contract"))
		results, err := services.Sequencer.WaitUntilResults(t.Context(), pid)
		c.Assert(err, qt.IsNil)
		c.Logf("Results calculated: %v, waiting for onchain results...", results)

		var pubResults []*types.BigInt
		if err := helpers.TestWaitForWithChannel(timeoutCh, time.Second*10, func() bool {
			pubResults, err = helpers.TestResultsOnChain(services.Contracts, pid)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published results from contract"))
			return pubResults != nil
		}); err != nil {
			c.Fatalf("Timeout waiting for votes to be processed and published at contract")
			c.FailNow()
		}
		t.Logf("Results published: %v", pubResults)
	})
}
