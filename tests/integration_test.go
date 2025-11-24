package tests

import (
	"context"
	"math/big"
	"net/http"
	"os"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

func TestIntegration(t *testing.T) {
	numBallots := 5
	c := qt.New(t)

	// Setup
	ctx := t.Context()

	censusCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	_, port := services.API.HostPort()
	cli, err := NewTestClient(port)
	c.Assert(err, qt.IsNil)

	var (
		pid           *types.ProcessID
		encryptionKey *types.EncryptionKey
		ballotMode    *types.BallotMode
		signers       []*ethereum.Signer
		censusRoot    []byte
		censusURI     string
	)

	c.Run("create process", func(c *qt.C) {
		// Create census with numBallot participants
		// censusRoot, participants, signers, err = createCensus(cli, numBallots)
		censusRoot, censusURI, signers, err = createCensus(censusCtx, numBallots)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create census"))
		ballotMode = &types.BallotMode{
			NumFields:      circuits.MockNumFields,
			UniqueValues:   circuits.MockUniqueValues == 1,
			MaxValue:       new(types.BigInt).SetUint64(circuits.MockMaxValue),
			MinValue:       new(types.BigInt).SetUint64(circuits.MockMinValue),
			MaxValueSum:    new(types.BigInt).SetUint64(circuits.MockMaxValueSum),
			MinValueSum:    new(types.BigInt).SetUint64(circuits.MockMinValueSum),
			CostFromWeight: circuits.MockCostFromWeight == 1,
			CostExponent:   circuits.MockCostExponent,
		}

		if !isCSPCensus() {
			// first try to reproduce some bugs we had in sequencer in the past
			// but only if we are not using a CSP census
			{
				// create a different censusRoot for testing
				root2, root2URI, _, err := createCensus(censusCtx, numBallots*2)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to create census"))
				// createProcessInSequencer should be idempotent, but there was
				// a bug in this, test it's fixed
				pid1, encryptionKey1, stateRoot1, err := createProcessInSequencer(services.Contracts, cli, testCensusOrigin(), root2URI, root2, ballotMode)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))
				pid2, encryptionKey2, stateRoot2, err := createProcessInSequencer(services.Contracts, cli, testCensusOrigin(), root2URI, root2, ballotMode)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))
				c.Assert(pid2.String(), qt.Equals, pid1.String())
				c.Assert(encryptionKey2, qt.DeepEquals, encryptionKey1)
				c.Assert(stateRoot2.String(), qt.Equals, stateRoot1.String())
				// a subsequent call to create process, same processID but with
				// different censusOrigin should return the same encryptionKey
				// but yield a different stateRoot
				pid3, encryptionKey3, stateRoot3, err := createProcessInSequencer(services.Contracts, cli, testWrongCensusOrigin(), root2URI, root2, ballotMode)
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
		var stateRoot *types.HexBytes
		pid, encryptionKey, stateRoot, err = createProcessInSequencer(services.Contracts, cli, testCensusOrigin(), censusURI, censusRoot, ballotMode)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))

		// now create process in contracts
		pid2, err := createProcessInContracts(services.Contracts, testCensusOrigin(), censusURI, censusRoot, ballotMode, encryptionKey, stateRoot)
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

	CreateProcessLoop:
		for {
			select {
			case <-createProcessCtx.Done():
				c.Fatal("Timeout waiting for process to be created and registered")
				c.FailNow()
			default:
				if _, err := services.Storage.Process(pid); err == nil {
					break CreateProcessLoop
				}
				time.Sleep(time.Millisecond * 200)
			}
		}
		t.Logf("Process ID: %s", pid.String())

		// Wait for the process to be registered in the sequencer
		for {
			select {
			case <-createProcessCtx.Done():
				c.Fatal("Timeout waiting for process to be registered in sequencer")
				c.FailNow()
			default:
				if services.Sequencer.ExistsProcessID(pid.Marshal()) {
					t.Logf("Process ID %s registered in sequencer", pid.String())
					return
				}
				time.Sleep(time.Millisecond * 200)
			}
		}
	})

	// Store the voteIDs returned from the API to check their status later
	var voteIDs []types.HexBytes
	var ks []*big.Int

	c.Run("create votes", func(c *qt.C) {
		count := 0
		for i := range signers {
			// generate a vote for the first participant
			k := util.RandomBigInt(big.NewInt(100000000), big.NewInt(9999999999999999))
			vote, err := createVoteWithRandomFields(pid, ballotMode, encryptionKey, signers[i], k)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			if isCSPCensus() {
				censusProof, err := generateCensusProof(cli, censusRoot, pid.Marshal(), signers[i].Address().Bytes())
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
			count++
		}
		c.Assert(count, qt.Equals, numBallots)
		// Wait for the vote to be registered
		t.Logf("Waiting for %d votes to be registered and aggregated", count)
	})

	c.Assert(ks, qt.HasLen, numBallots)
	c.Assert(voteIDs, qt.HasLen, numBallots)

	c.Run("create invalid votes", func(c *qt.C) {
		vote, err := createVoteFromInvalidVoter(pid, ballotMode, encryptionKey)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote from invalid voter"))
		// Make the request to try cast the vote
		body, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, 400)
		c.Assert(string(body), qt.Contains, api.ErrInvalidCensusProof.Error())
	})

	c.Run("try to overwrite valid votes", func(c *qt.C) {
		for i := range signers {
			// generate a vote for the participant
			vote, err := createVoteWithRandomFields(pid, ballotMode, encryptionKey, signers[i], ks[i])
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			// generate census proof for the participant
			if isCSPCensus() {
				censusProof, err := generateCensusProof(cli, censusRoot, pid.Marshal(), signers[i].Address().Bytes())
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))
				c.Assert(censusProof, qt.Not(qt.IsNil))
				vote.CensusProof = *censusProof
			}
			// Make the request to cast the vote
			body, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, http.StatusConflict)
			c.Assert(string(body), qt.Contains, api.ErrBallotAlreadyProcessing.Error())
		}
	})

	// Set up timeout based on context deadline
	var timeoutCh <-chan time.Time
	deadline, hasDeadline := t.Deadline()

	if hasDeadline {
		// If context has a deadline, set timeout to 15 seconds before it
		// to allow for clean shutdown and error reporting
		remainingTime := time.Until(deadline)
		timeoutBuffer := 15 * time.Second

		// If we have less than the buffer time left, use half of the remaining time
		if remainingTime <= timeoutBuffer {
			timeoutBuffer = remainingTime / 2
		}

		effectiveTimeout := remainingTime - timeoutBuffer
		timeoutCh = time.After(effectiveTimeout)
		t.Logf("Test will timeout in %v (deadline: %v)", effectiveTimeout, deadline)
	} else {
		// No deadline set, use a reasonable default
		timeOut := 20 * time.Minute
		if os.Getenv("DEBUG") != "" && os.Getenv("DEBUG") != "false" {
			timeOut = 50 * time.Minute
		}
		timeoutCh = time.After(timeOut)
		t.Logf("No test deadline found, using %s minute default timeout", timeOut.String())
	}

	c.Run("wait for process votes", func(c *qt.C) {
		// Create a ticker to check the status of votes every 10 seconds
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
	SettledVotesLoop:
		for {
			select {
			case <-ticker.C:
				// Check that votes are settled (state transitions confirmed on blockchain)
				if allSettled, failed, err := checkVoteStatus(cli, pid, voteIDs, storage.VoteIDStatusName(storage.VoteIDStatusSettled)); !allSettled {
					c.Assert(err, qt.IsNil, qt.Commentf("Failed to check vote status"))
					if len(failed) > 0 {
						hexFailed := make([]string, len(failed))
						for i, v := range failed {
							hexFailed[i] = v.String()
						}
						t.Fatalf("Some votes failed to be settled: %v", hexFailed)
					}
				}
				published, err := publishedVotes(services.Contracts, pid)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published votes from contract"))
				if published < numBallots {
					continue
				}
				break SettledVotesLoop
			case <-timeoutCh:
				c.Fatalf("Timeout waiting for votes to be settled and published at contract")
			}
		}
		t.Log("All votes settled.")
	})

	processedVotes := []api.Vote{}
	voteIDs = []types.HexBytes{}
	c.Run("overwrite valid votes", func(c *qt.C) {
		count := 0
		for i := range signers {
			// generate a vote for the participant
			vote, err := createVoteWithRandomFields(pid, ballotMode, encryptionKey, signers[i], nil)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			// generate census proof for the participant
			if isCSPCensus() {
				censusProof, err := generateCensusProof(cli, censusRoot, pid.Marshal(), signers[i].Address().Bytes())
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
			processedVotes = append(processedVotes, vote)
			count++
		}
		c.Assert(count, qt.Equals, numBallots)
		// Wait for the vote to be registered
		t.Logf("Waiting for %d votes to be registered and aggregated", count)
	})

	c.Run("wait for process overwrite votes", func(c *qt.C) {
		// Create a ticker to check the status of votes every 10 seconds
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
	ResultsLoop2:
		for {
			select {
			case <-ticker.C:
				// Check that votes are settled (state transitions confirmed on blockchain)
				allSettled, failed, err := checkVoteStatus(cli, pid, voteIDs, storage.VoteIDStatusName(storage.VoteIDStatusSettled))
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
				publishedOverwrite, err := publishedOverwriteVotes(services.Contracts, pid)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published overwrite votes from contract"))
				if publishedOverwrite < numBallots {
					continue
				}
				break ResultsLoop2
			case <-timeoutCh:
				c.Fatalf("Timeout waiting for overwrite votes to be processed and published at contract")
			}
		}
		t.Log("All overwrite votes processed, finalizing process...")
	})

	c.Run("wait for publish votes", func(c *qt.C) {
		err := finishProcessOnContract(services.Contracts, pid)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to finish process on contract"))
		results, err := services.Sequencer.WaitUntilResults(t.Context(), pid)
		c.Assert(err, qt.IsNil)
		c.Logf("Results calculated: %v, waiting for onchain results...", results)

		// Create a ticker to check the status of votes every 10 seconds
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				results, err := publishedResults(services.Contracts, pid)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published results from contract"))
				if results == nil {
					t.Log("Results not yet published, waiting...")
					continue
				}
				t.Logf("Results published: %v", results)
				return
			case <-timeoutCh:
				c.Fatalf("Timeout waiting for votes to be processed and published at contract")
			}
		}
	})

	c.Run("try to send votes to ended process", func(c *qt.C) {
		for i := range signers {
			// generate a vote for the first participant
			vote, err := createVoteWithRandomFields(pid, ballotMode, encryptionKey, signers[i], nil)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			// generate census proof for the participant
			if isCSPCensus() {
				censusProof, err := generateCensusProof(cli, censusRoot, pid.Marshal(), signers[i].Address().Bytes())
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))
				c.Assert(censusProof, qt.Not(qt.IsNil))
				vote.CensusProof = *censusProof
			}
			// Make the request to cast the vote
			body, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, 400)
			c.Assert(string(body), qt.Contains, api.ErrProcessNotAcceptingVotes.Error())
		}
	})
}
