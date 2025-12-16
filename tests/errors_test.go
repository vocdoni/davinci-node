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
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/tests/helpers"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

func TestErrors(t *testing.T) {
	// Install log monitor that panics on Error level logs
	previousLogger := log.EnablePanicOnError(t.Name())
	defer log.RestoreLogger(previousLogger)

	numVoters := 2
	c := qt.New(t)

	var (
		err           error
		pid           *types.ProcessID
		stateRoot     *types.HexBytes
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

	c.Run("create census", func(c *qt.C) {
		censusCtx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Create census with numVoters participants
		censusRoot, censusURI, signers, err = helpers.TestCensusWithRandomVoters(censusCtx, types.CensusOriginMerkleTreeOffchainStaticV1, numVoters)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create census"))
		c.Assert(len(signers), qt.Equals, numVoters)
	})

	c.Run("same state root with different process parameters", func(c *qt.C) {
		// createProcessInSequencer should be idempotent, but there was
		// a bug in this, test it's fixed
		pid1, encryptionKey1, stateRoot1, err := helpers.TestNewProcess(services.Contracts, services.HTTPClient, helpers.TestCensusOrigin(), censusURI, censusRoot, defaultBallotMode)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))
		pid2, encryptionKey2, stateRoot2, err := helpers.TestNewProcess(services.Contracts, services.HTTPClient, helpers.TestCensusOrigin(), censusURI, censusRoot, defaultBallotMode)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))
		c.Assert(pid2.String(), qt.Equals, pid1.String())
		c.Assert(encryptionKey2, qt.DeepEquals, encryptionKey1)
		c.Assert(stateRoot2.String(), qt.Equals, stateRoot1.String())
		// a subsequent call to create process, same processID but with
		// different censusOrigin should return the same encryptionKey
		// but yield a different stateRoot
		pid3, encryptionKey3, stateRoot3, err := helpers.TestNewProcess(services.Contracts, services.HTTPClient, helpers.TestWrongCensusOrigin(), censusURI, censusRoot, defaultBallotMode)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))
		c.Assert(pid3.String(), qt.Equals, pid1.String())
		c.Assert(encryptionKey3, qt.DeepEquals, encryptionKey1)
		c.Assert(stateRoot3.String(), qt.Not(qt.Equals), stateRoot1.String(), qt.Commentf("sequencer is returning the same state root although process parameters changed"))
	})

	c.Run("create process", func(c *qt.C) {
		// create process in the sequencer
		pid, encryptionKey, stateRoot, err = helpers.TestNewProcess(services.Contracts, services.HTTPClient, types.CensusOriginMerkleTreeOffchainStaticV1, censusURI, censusRoot, defaultBallotMode)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))

		// now create process in contracts
		onchainPID, err := helpers.TestProcessOnChain(services.Contracts, types.CensusOriginMerkleTreeOffchainStaticV1, censusURI, censusRoot, defaultBallotMode, encryptionKey, stateRoot, numVoters)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in contracts"))
		c.Assert(onchainPID.String(), qt.Equals, pid.String())

		if err := helpers.TestWaitForWithChannel(timeoutCh, time.Millisecond*200, func() bool {
			_, err := services.Storage.Process(pid)
			return err == nil
		}); err != nil {
			c.Fatal("Timeout waiting for process to be created in storage")
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

	c.Run("create invalid votes", func(c *qt.C) {
		vote, err := helpers.TestNewVoteFromNonCensusVoter(pid, defaultBallotMode, encryptionKey)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote from invalid voter"))
		// Make the request to try cast the vote
		body, status, err := services.HTTPClient.Request("POST", vote, nil, api.VotesEndpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, api.ErrInvalidCensusProof.HTTPstatus)
		c.Assert(string(body), qt.Contains, api.ErrInvalidCensusProof.Error())
	})

	c.Run("create votes", func(c *qt.C) {
		for i, signer := range signers {
			// generate a vote for the first participant
			k := util.RandomBigInt(big.NewInt(100000000), big.NewInt(9999999999999999))
			vote, err := helpers.TestNewVoteWithRandomFields(pid, defaultBallotMode, encryptionKey, signer, k)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			// generate census proof
			vote.CensusProof, err = helpers.TestCensusProof(types.CensusOriginMerkleTreeOffchainStaticV1, pid.Marshal(), signers[i].Address().Bytes())
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

	c.Run("try to overwrite valid votes", func(c *qt.C) {
		for i, signer := range signers {
			// generate a vote for the participant
			vote, err := helpers.TestNewVoteWithRandomFields(pid, defaultBallotMode, encryptionKey, signer, ks[i])
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			// generate census proof for the participant
			vote.CensusProof, err = helpers.TestCensusProof(types.CensusOriginMerkleTreeOffchainStaticV1, pid.Marshal(), signers[i].Address().Bytes())
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))
			// Make the request to cast the vote
			body, status, err := services.HTTPClient.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, api.ErrBallotAlreadyProcessing.HTTPstatus)
			c.Assert(string(body), qt.Contains, api.ErrBallotAlreadyProcessing.Error())
		}
	})

	c.Run("wait for settled votes", func(c *qt.C) {
		t.Logf("Waiting for %d votes to be settled", numVoters)
		if err := helpers.TestWaitForWithChannel(timeoutCh, 10*time.Second, func() bool {
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
			return votersCount == numVoters
		}); err != nil {
			c.Fatalf("Timeout waiting for votes to be settled and published at contract")
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
		if err := helpers.TestWaitForWithChannel(timeoutCh, 10*time.Second, func() bool {
			pubResults, err = helpers.TestResultsOnChain(services.Contracts, pid)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published results from contract"))
			return pubResults != nil
		}); err != nil {
			c.Fatalf("Timeout waiting for votes to be processed and published at contract")
			c.FailNow()
		}
		t.Logf("Results published: %v", pubResults)
	})

	c.Run("try to send votes to ended process", func(c *qt.C) {
		for i := range signers {
			// generate a vote for the first participant
			vote, err := helpers.TestNewVoteWithRandomFields(pid, defaultBallotMode, encryptionKey, signers[i], nil)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			// generate census proof for the participant
			vote.CensusProof, err = helpers.TestCensusProof(types.CensusOriginMerkleTreeOffchainStaticV1, pid.Marshal(), signers[i].Address().Bytes())
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))
			// Make the request to cast the vote
			body, status, err := services.HTTPClient.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, api.ErrProcessNotAcceptingVotes.HTTPstatus)
			c.Assert(string(body), qt.Contains, api.ErrProcessNotAcceptingVotes.Error())
		}
	})
}
