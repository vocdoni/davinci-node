package tests

import (
	"context"
	"math/big"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/prover/debug"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/tests/helpers"
	"github.com/vocdoni/davinci-node/types"
)

func TestIntegration(t *testing.T) {
	// Install log monitor that panics on Error level logs
	previousLogger := log.EnablePanicOnError(t.Name())
	defer log.RestoreLogger(previousLogger)

	numVoters := 2
	c := qt.New(t)

	// Setup
	ctx := t.Context()

	censusCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	_, port := services.API.HostPort()
	cli, err := newTestClient(port)
	c.Assert(err, qt.IsNil)

	var (
		pid           types.ProcessID
		stateRoot     *types.HexBytes
		encryptionKey *types.EncryptionKey
		ballotMode    *types.BallotMode
		signers       []*ethereum.Signer
		censusRoot    []byte
		censusURI     string
	)

	if helpers.IsDebugTest() {
		services.Sequencer.SetProver(debug.NewDebugProver(t))
	}

	c.Run("create process", func(c *qt.C) {
		// Create census with numVoters participants
		censusRoot, censusURI, signers, err = helpers.TestCensusWithRandomVoters(censusCtx, types.CensusOriginMerkleTreeOffchainStaticV1, numVoters+1)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create census"))
		ballotMode = testutil.BallotModeInternal()

		if !helpers.IsCSPCensus() {
			// first try to reproduce some bugs we had in sequencer in the past
			// but only if we are not using a CSP census
			{
				// create a different censusRoot for testing
				root2, root2URI, _, err := helpers.TestCensusWithRandomVoters(censusCtx, types.CensusOriginMerkleTreeOffchainStaticV1, numVoters*2)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to create census"))
				// createProcessInSequencer should be idempotent, but there was
				// a bug in this, test it's fixed
				pid1, encryptionKey1, stateRoot1, err := helpers.TestNewProcess(services.Contracts, cli, helpers.TestCensusOrigin(), root2URI, root2, ballotMode)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))
				pid2, encryptionKey2, stateRoot2, err := helpers.TestNewProcess(services.Contracts, cli, helpers.TestCensusOrigin(), root2URI, root2, ballotMode)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))
				c.Assert(pid2.String(), qt.Equals, pid1.String())
				c.Assert(encryptionKey2, qt.DeepEquals, encryptionKey1)
				c.Assert(stateRoot2.String(), qt.Equals, stateRoot1.String())
				// a subsequent call to create process, same processID but with
				// different censusOrigin should return the same encryptionKey
				// but yield a different stateRoot
				pid3, encryptionKey3, stateRoot3, err := helpers.TestNewProcess(services.Contracts, cli, helpers.TestWrongCensusOrigin(), root2URI, root2, ballotMode)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))
				c.Assert(pid3.String(), qt.Equals, pid1.String())
				c.Assert(encryptionKey3, qt.DeepEquals, encryptionKey1)
				c.Assert(stateRoot3.String(), qt.Not(qt.Equals), stateRoot1.String(),
					qt.Commentf("sequencer is returning the same state root although process parameters changed"))
			}
		}
		// this final call is the good one, with the real censusRoot, should
		// return the correct stateRoot and encryptionKey that we'll use to
		// create process in contracts
		pid, encryptionKey, stateRoot, err = helpers.TestNewProcess(services.Contracts, cli, helpers.TestCensusOrigin(), censusURI, censusRoot, ballotMode)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))

		// now create process in contracts
		pid2, err := helpers.TestProcessOnChain(services.Contracts, helpers.TestCensusOrigin(), censusURI, censusRoot, ballotMode, encryptionKey, stateRoot, numVoters)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in contracts"))
		c.Assert(pid2.String(), qt.Equals, pid.String())

		// create a timeout for the process creation, if it is greater than the
		// test timeout use the test timeout
		createProcessTimeout := time.Minute * 2
		if timeout, hasDeadline := t.Deadline(); hasDeadline {
			remainingTime := time.Until(timeout)
			if remainingTime < createProcessTimeout {
				createProcessTimeout = remainingTime
			}
		}
		// Wait for the process to be registered
		createProcessCtx, cancel := context.WithTimeout(ctx, createProcessTimeout)
		defer cancel()

		if err := helpers.TestWaitForWithContext(createProcessCtx, time.Millisecond*200, func() bool {
			_, err := services.Storage.Process(pid)
			return err == nil
		}); err != nil {
			c.Fatal("Timeout waiting for process to be created in storage")
			c.FailNow()
		}
		t.Logf("Process ID: %s", pid.String())

		// Wait for the process to be registered in the sequencer
		if err := helpers.TestWaitForWithContext(createProcessCtx, time.Millisecond*200, func() bool {
			return services.Sequencer.ExistsProcessID(pid)
		}); err != nil {
			c.Fatal("Timeout waiting for process to be registered in sequencer")
			c.FailNow()
		}
	})

	// Store the voteIDs returned from the API to check their status later
	var voteIDs []types.HexBytes
	var ks []*big.Int

	c.Run("create votes", func(c *qt.C) {
		c.Assert(len(signers), qt.Equals, numVoters+1)
		for i := range signers[:numVoters] {
			// generate a vote for the first participant
			k, err := elgamal.RandK()
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate random k for ballot %d", i))
			vote, err := helpers.TestNewVoteWithRandomFields(pid, ballotMode, encryptionKey, signers[i], k)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			if helpers.IsCSPCensus() {
				censusProof, err := helpers.TestCensusProof(pid, signers[i].Address().Bytes())
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))
				c.Assert(censusProof, qt.Not(qt.IsNil))
				vote.CensusProof = *censusProof
			}
			// Make the request to cast the vote
			_, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, 200)

			// Save the voteID for status checks
			voteIDs = append(voteIDs, vote.VoteID)
			ks = append(ks, k)
		}
		// Wait for the vote to be registered
		t.Logf("Waiting for %d votes to be settled", numVoters)
	})

	c.Assert(ks, qt.HasLen, numVoters)
	c.Assert(voteIDs, qt.HasLen, numVoters)

	c.Run("create invalid votes", func(c *qt.C) {
		vote, err := helpers.TestNewVoteFromUnknownVoter(pid, ballotMode, encryptionKey)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote from invalid voter"))
		// Make the request to try cast the vote
		body, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, api.ErrInvalidCensusProof.HTTPstatus)
		c.Assert(string(body), qt.Contains, api.ErrInvalidCensusProof.Error())
	})

	c.Run("try to overwrite valid votes", func(c *qt.C) {
		for i := range signers[:numVoters] {
			// generate a vote for the participant
			vote, err := helpers.TestNewVoteWithRandomFields(pid, ballotMode, encryptionKey, signers[i], ks[i])
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			// generate census proof for the participant
			if helpers.IsCSPCensus() {
				censusProof, err := helpers.TestCensusProof(pid, signers[i].Address().Bytes())
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))
				c.Assert(censusProof, qt.Not(qt.IsNil))
				vote.CensusProof = *censusProof
			}
			// Make the request to cast the vote
			body, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, api.ErrBallotAlreadyProcessing.HTTPstatus)
			c.Assert(string(body), qt.Contains, api.ErrBallotAlreadyProcessing.Error())
		}
	})

	timeoutCh := helpers.TestTimeoutChan(t)

	c.Run("wait for process votes", func(c *qt.C) {
		if err := helpers.TestWaitForWithChannel(timeoutCh, 10*time.Second, func() bool {
			// Check that votes are settled (state transitions confirmed on blockchain)
			if allSettled, failed, err := helpers.TestEnsureVotesStatus(cli, pid, voteIDs, storage.VoteIDStatusName(storage.VoteIDStatusSettled)); !allSettled {
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to check vote status"))
				if len(failed) > 0 {
					hexFailed := make([]string, len(failed))
					for i, v := range failed {
						hexFailed[i] = v.String()
					}
					t.Fatalf("Some votes failed to be settled: %v", hexFailed)
				}
			}
			votersCount, err := helpers.TestProcessVotersCount(services.Contracts, pid)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published votes from contract"))
			return votersCount >= numVoters
		}); err != nil {
			c.Fatalf("Timeout waiting for votes to be settled and published at contract")
			c.FailNow()
		}
		t.Log("All votes settled.")
	})

	c.Run("wait until the stateroot is updated", func(c *qt.C) {
		if err := helpers.TestWaitForWithChannel(timeoutCh, 10*time.Second, func() bool {
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

	voteIDs = []types.HexBytes{}
	c.Run("try to create a new vote even the maxVoters is reached", func(c *qt.C) {
		extraSigner := signers[numVoters] // get an extra signer from the created census
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create new signer"))
		// generate a vote for the new participant
		vote, err := helpers.TestNewVoteWithRandomFields(pid, ballotMode, encryptionKey, extraSigner, nil)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
		// generate census proof for the participant
		if helpers.IsCSPCensus() {
			censusProof, err := helpers.TestCensusProof(pid, extraSigner.Address().Bytes())
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))
			c.Assert(censusProof, qt.Not(qt.IsNil))
			vote.CensusProof = *censusProof
		}
		// Make the request to cast the vote
		body, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, api.ErrProcessMaxVotersReached.HTTPstatus)
		c.Assert(string(body), qt.Contains, api.ErrProcessMaxVotersReached.Error())

		// Set the max voters to a higher number to allow new votes
		err = helpers.TestUpdateMaxVotersOnChain(services.Contracts, pid, numVoters+1)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to update max voters"))

		// Wait 15 seconds for the process monitor to pick up the change
		time.Sleep(15 * time.Second)

		// Make the request to cast the vote again
		_, status, err = cli.Request("POST", vote, nil, api.VotesEndpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, 200)

		// append the new vote stuff to the lists for later checks
		voteIDs = append(voteIDs, vote.VoteID)
	})

	c.Run("overwrite valid votes", func(c *qt.C) {
		for i := range signers[:numVoters] {
			// generate a vote for the participant
			vote, err := helpers.TestNewVoteWithRandomFields(pid, ballotMode, encryptionKey, signers[i], nil)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			// generate census proof for the participant
			if helpers.IsCSPCensus() {
				censusProof, err := helpers.TestCensusProof(pid, signers[i].Address().Bytes())
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))
				c.Assert(censusProof, qt.Not(qt.IsNil))
				vote.CensusProof = *censusProof
			}
			// Make the request to cast the vote
			_, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, 200)
			c.Logf("Vote %d (addr: %s) created with ID: %s", i, vote.Address.String(), vote.VoteID.String())

			// Save the voteID for status checks
			voteIDs = append(voteIDs, vote.VoteID)
		}
		// Wait for the vote to be registered
		t.Logf("Waiting for %d votes to be settled", len(voteIDs))
	})

	c.Run("wait for process overwrite votes", func(c *qt.C) {
		if err := helpers.TestWaitForWithChannel(timeoutCh, 10*time.Second, func() bool {
			// Check that votes are settled (state transitions confirmed on blockchain)
			allSettled, failed, err := helpers.TestEnsureVotesStatus(cli, pid, voteIDs, storage.VoteIDStatusName(storage.VoteIDStatusSettled))
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to check overwrite vote status"))
			if !allSettled {
				if len(failed) > 0 {
					hexFailed := make([]string, len(failed))
					for i, v := range failed {
						hexFailed[i] = v.String()
					}
					t.Fatalf("Some overwrite votes failed to be processed: %v", hexFailed)
				}
			}
			votersCount, err := helpers.TestProcessVotersCount(services.Contracts, pid)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published votes from contract"))
			overwrittenVotes, err := helpers.TestProcessOverwrittenVotesCount(services.Contracts, pid)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to get count of overwritten votes from contract"))
			return overwrittenVotes >= numVoters && votersCount >= numVoters+1
		}); err != nil {
			c.Fatalf("Timeout waiting for overwrite votes to be settled and published at contract")
			c.FailNow()
		}
		t.Log("All overwrite votes processed, finalizing process...")
	})

	c.Run("wait for publish votes", func(c *qt.C) {
		err := helpers.TestFinishProcessOnChain(services.Contracts, pid)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to finish process on contract"))
		results, err := services.Sequencer.WaitUntilResults(t.Context(), pid)
		c.Assert(err, qt.IsNil)
		c.Logf("Results calculated: %v, waiting for onchain results...", results)

		// Create a ticker to check the status of votes every 10 seconds
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		var pubResults []*types.BigInt
		if err := helpers.TestWaitForWithChannel(timeoutCh, 10*time.Second, func() bool {
			pubResults, err := helpers.TestResultsOnChain(services.Contracts, pid)
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
			vote, err := helpers.TestNewVoteWithRandomFields(pid, ballotMode, encryptionKey, signers[i], nil)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			// generate census proof for the participant
			if helpers.IsCSPCensus() {
				censusProof, err := helpers.TestCensusProof(pid, signers[i].Address().Bytes())
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))
				c.Assert(censusProof, qt.Not(qt.IsNil))
				vote.CensusProof = *censusProof
			}
			// Make the request to cast the vote
			body, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, api.ErrProcessNotAcceptingVotes.HTTPstatus)
			c.Assert(string(body), qt.Contains, api.ErrProcessNotAcceptingVotes.Error())
		}
	})
}
