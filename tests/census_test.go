package tests

import (
	"bytes"
	"context"
	"math/big"
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

func TestDynamicOffChainCensus(t *testing.T) {
	numInitialVoters := 2
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
		// Create census with numVoters participants
		censusRoot, censusURI, signers, err = createCensusWithRandomVoters(censusCtx, types.CensusOriginMerkleTreeOffchainDynamicV1, numInitialVoters)
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

		// create process in sequencer
		var stateRoot *types.HexBytes
		pid, encryptionKey, stateRoot, err = createProcessInSequencer(services.Contracts, cli, testCensusOrigin(), censusURI, censusRoot, ballotMode)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))

		// now create process in contracts
		pid2, err := createProcessInContracts(services.Contracts, testCensusOrigin(), censusURI, censusRoot, ballotMode, encryptionKey, stateRoot, numInitialVoters)
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
		c.Assert(len(signers), qt.Equals, numInitialVoters)
		for i := range signers {
			// generate a vote for the first participant
			k := util.RandomBigInt(big.NewInt(100000000), big.NewInt(9999999999999999))
			vote, err := createVoteWithRandomFields(pid, ballotMode, encryptionKey, signers[i], k)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
			if isCSPCensus() {
				censusProof, err := generateCensusProof(pid.Marshal(), signers[i].Address().Bytes())
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
		t.Logf("Waiting for %d votes to be registered and aggregated", numInitialVoters)
	})

	c.Assert(ks, qt.HasLen, numInitialVoters)
	c.Assert(voteIDs, qt.HasLen, numInitialVoters)

	c.Run("update the census", func(c *qt.C) {
		// create a signer that is not in the census
		signer, err := ethereum.NewSigner()
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create ethereum signer"))
		// try to vote with the new signer, should fail
		k := util.RandomBigInt(big.NewInt(100000000), big.NewInt(9999999999999999))
		vote, err := createVoteWithRandomFields(pid, ballotMode, encryptionKey, signer, k)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create vote"))
		if isCSPCensus() {
			censusProof, err := generateCensusProof(pid.Marshal(), signer.Address().Bytes())
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate census proof"))
			c.Assert(censusProof, qt.Not(qt.IsNil))
			vote.CensusProof = *censusProof
		}
		// Make the request to cast the vote
		body, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, api.ErrInvalidCensusProof.HTTPstatus)
		c.Assert(string(body), qt.Contains, api.ErrInvalidCensusProof.Error())

		// create a new census including the new signer
		signers = append(signers, signer)
		censusRoot, censusURI, _, err = createCensusWithVoters(censusCtx, types.CensusOriginMerkleTreeOffchainDynamicV1, signers...)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create census"))

		// update the census in the contracts
		err = updateProcessCensusInContracts(services.Contracts, pid, types.Census{
			CensusOrigin: testCensusOrigin(),
			CensusRoot:   censusRoot,
			CensusURI:    censusURI,
		})
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to update process census in contracts"))

		// wait to new census in the sequencer
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			// Get the process from storage
			process, err := services.Storage.Process(pid)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to get process from storage"))
			if !bytes.Equal(process.Census.CensusRoot, censusRoot) {
				continue
			}
			t.Log("Process census root  updated")
			break
		}

		// Make the request to cast the vote
		_, status, err = cli.Request("POST", vote, nil, api.VotesEndpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, 200)

		// Save the voteID for status checks
		voteIDs = append(voteIDs, vote.VoteID)
		ks = append(ks, k)
	})

	timeoutCh := testTimeoutChan(t)

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
				votersCount, err := votersCount(services.Contracts, pid)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to get published votes from contract"))
				if votersCount < len(voteIDs) {
					continue
				}
				break SettledVotesLoop
			case <-timeoutCh:
				c.Fatalf("Timeout waiting for votes to be settled and published at contract")
			}
		}
		t.Log("All votes settled.")
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
}
