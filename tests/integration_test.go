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
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
	"github.com/vocdoni/davinci-node/workers"
)

const (
	testWorkerSeed    = "test-seed"
	testWorkerTimeout = time.Second * 5
)

func TestMain(m *testing.M) {
	log.Init(log.LogLevelDebug, "stdout", nil)
	if err := service.DownloadArtifacts(30*time.Minute, ""); err != nil {
		log.Errorw(err, "failed to download artifacts")
		return
	}
	os.Exit(m.Run())
}

func TestIntegration(t *testing.T) {
	numBallots := 5
	c := qt.New(t)

	// Setup
	ctx := t.Context()
	services := NewTestService(t, ctx, testWorkerSeed, testWorkerTimeout, workers.DefaultWorkerBanRules)
	_, port := services.API.HostPort()
	cli, err := NewTestClient(port)
	c.Assert(err, qt.IsNil)

	var (
		pid           *types.ProcessID
		encryptionKey *types.EncryptionKey
		ballotMode    *types.BallotMode
		signers       []*ethereum.Signer
		proofs        []*types.CensusProof
		root          []byte
		participants  []*api.CensusParticipant
	)

	c.Run("create organization", func(c *qt.C) {
		orgAddr := createOrganization(c, services.Contracts)
		t.Logf("Organization address: %s", orgAddr.String())
	})

	c.Run("create process", func(c *qt.C) {
		// Create census with numBallot participants
		root, participants, signers = createCensus(c, cli, numBallots)

		// Generate proof for first participant
		proofs = make([]*types.CensusProof, numBallots)
		for i := range participants {
			proofs[i] = generateCensusProof(c, cli, root, participants[i].Key)
			c.Assert(proofs[i], qt.Not(qt.IsNil))
			c.Assert(proofs[i].Siblings, qt.IsNotNil)
		}
		// Check the first proof key is the same as the participant key and signer address
		qt.Assert(t, proofs[0].Key.String(), qt.DeepEquals, participants[0].Key.String())
		qt.Assert(t, string(proofs[0].Key), qt.DeepEquals, string(signers[0].Address().Bytes()))

		ballotMode = &types.BallotMode{
			MaxCount:        circuits.MockMaxCount,
			ForceUniqueness: circuits.MockForceUniqueness == 1,
			MaxValue:        new(types.BigInt).SetUint64(circuits.MockMaxValue),
			MinValue:        new(types.BigInt).SetUint64(circuits.MockMinValue),
			MaxTotalCost:    new(types.BigInt).SetUint64(circuits.MockMaxTotalCost),
			MinTotalCost:    new(types.BigInt).SetUint64(circuits.MockMinTotalCost),
			CostFromWeight:  circuits.MockCostFromWeight == 1,
			CostExponent:    circuits.MockCostExp,
		}

		// first try to reproduce some bugs we had in sequencer in the past
		{
			// create a different censusRoot for testing
			root2, _, _ := createCensus(c, cli, numBallots*2)
			// createProcessInSequencer should be idempotent, but there was a bug in this. Test it's fixed
			pid1, encryptionKey1, stateRoot1 := createProcessInSequencer(c, services.Contracts, cli, root2, ballotMode)
			pid2, encryptionKey2, stateRoot2 := createProcessInSequencer(c, services.Contracts, cli, root2, ballotMode)
			c.Assert(pid2.String(), qt.Equals, pid1.String())
			c.Assert(encryptionKey2, qt.DeepEquals, encryptionKey1)
			c.Assert(stateRoot2.String(), qt.Equals, stateRoot1.String())
			// a subsequent call to create process, same processID but with different censusRoot
			// should return the same encryptionKey but yield a different stateRoot.
			pid3, encryptionKey3, stateRoot3 := createProcessInSequencer(c, services.Contracts, cli, root, ballotMode)
			c.Assert(pid3.String(), qt.Equals, pid1.String())
			c.Assert(encryptionKey3, qt.DeepEquals, encryptionKey1)
			c.Assert(stateRoot3.String(), qt.Not(qt.Equals), stateRoot1.String(),
				qt.Commentf("sequencer is returning the same state root although process parameters changed"))
		}
		// this final call is the good one, with the real censusRoot, should return the correct stateRoot and encryptionKey that
		// we'll use to create process in contracts
		var stateRoot *types.HexBytes
		pid, encryptionKey, stateRoot = createProcessInSequencer(c, services.Contracts, cli, root, ballotMode)

		// now create process in contracts
		pid2 := createProcessInContracts(c, services.Contracts, root, ballotMode, encryptionKey, stateRoot)
		c.Assert(pid2.String(), qt.Equals, pid.String())

		// create a timeout for the process creation, if it is greater than the test timeout
		// use the test timeout
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
			vote := createVoteWithRandomFields(c, pid, ballotMode, encryptionKey, signers[i], k)
			// generate census proof for first participant
			censusProof := generateCensusProof(c, cli, root, signers[i].Address().Bytes())
			c.Assert(censusProof, qt.Not(qt.IsNil))
			c.Assert(censusProof.Siblings, qt.IsNotNil)
			vote.CensusProof = *censusProof
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
		vote := createVoteFromInvalidVoter(c, pid, ballotMode, encryptionKey)
		// Make the request to try cast the vote
		body, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, 400)
		c.Assert(string(body), qt.Contains, api.ErrMalformedBody.Withf("invalid census proof").Error())
	})

	c.Run("try to overwrite valid votes", func(c *qt.C) {
		for i := range signers {
			// generate a vote for the first participant
			vote := createVoteWithRandomFields(c, pid, ballotMode, encryptionKey, signers[i], ks[i])
			// generate census proof for first participant
			censusProof := generateCensusProof(c, cli, root, signers[i].Address().Bytes())
			c.Assert(censusProof, qt.Not(qt.IsNil))
			c.Assert(censusProof.Siblings, qt.IsNotNil)
			vote.CensusProof = *censusProof
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
				if allSettled, failed := checkVoteStatus(t, cli, pid, voteIDs, storage.VoteIDStatusName(storage.VoteIDStatusSettled)); !allSettled {
					if len(failed) > 0 {
						hexFailed := make([]string, len(failed))
						for i, v := range failed {
							hexFailed[i] = v.String()
						}
						t.Fatalf("Some votes failed to be settled: %v", hexFailed)
					}
				}
				if publishedVotes(t, services.Contracts, pid) < numBallots {
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
			// generate a vote for the first participant
			vote := createVoteWithRandomFields(c, pid, ballotMode, encryptionKey, signers[i], nil)
			// generate census proof for first participant
			censusProof := generateCensusProof(c, cli, root, signers[i].Address().Bytes())
			c.Assert(censusProof, qt.Not(qt.IsNil))
			c.Assert(censusProof.Siblings, qt.IsNotNil)
			vote.CensusProof = *censusProof
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
				if allSettled, failed := checkVoteStatus(t, cli, pid, voteIDs, storage.VoteIDStatusName(storage.VoteIDStatusSettled)); !allSettled {
					if len(failed) > 0 {
						hexFailed := make([]string, len(failed))
						for i, v := range failed {
							hexFailed[i] = v.String()
						}
						t.Fatalf("Some overwrite votes failed to be processed: %v", hexFailed)
					}
				}
				if publishedOverwriteVotes(t, services.Contracts, pid) < numBallots {
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
		finishProcessOnContract(t, services.Contracts, pid)
		results, err := services.Sequencer.WaitUntilResults(t.Context(), pid)
		c.Assert(err, qt.IsNil)
		c.Logf("Results calculated: %v, waiting for onchain results...", results)

		// Create a ticker to check the status of votes every 10 seconds
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				results := publishedResults(t, services.Contracts, pid)
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
			vote := createVoteWithRandomFields(c, pid, ballotMode, encryptionKey, signers[i], nil)
			// generate census proof for first participant
			censusProof := generateCensusProof(c, cli, root, signers[i].Address().Bytes())
			c.Assert(censusProof, qt.Not(qt.IsNil))
			c.Assert(censusProof.Siblings, qt.IsNotNil)
			vote.CensusProof = *censusProof
			// Make the request to cast the vote
			body, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, 400)
			c.Assert(string(body), qt.Contains, api.ErrProcessNotAcceptingVotes.Error())
		}
	})
}
