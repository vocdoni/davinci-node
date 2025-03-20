package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/vocdoni-z-sandbox/api"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ethereum"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/service"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

func init() {
	log.Init(log.LogLevelDebug, "stdout", nil)
	if err := service.DownloadArtifacts(30 * time.Minute); err != nil {
		log.Errorw(err, "failed to download artifacts")
	}
}

func TestIntegration(t *testing.T) {
	numBallots := 2
	c := qt.New(t)

	// Setup
	ctx := context.Background()
	apiSrv, stg, contracts := NewTestService(t, ctx)
	_, port := apiSrv.HostPort()
	cli, err := NewTestClient(port)
	c.Assert(err, qt.IsNil)

	var (
		pid           *types.ProcessID
		encryptionKey *types.EncryptionKey
		ballotMode    *types.BallotMode
		signers       []*ethereum.SignKeys
		proofs        []*types.CensusProof
		root          []byte
		participants  []*api.CensusParticipant
	)

	c.Run("create organization", func(c *qt.C) {
		orgAddr := createOrganization(c, contracts)
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

		mockMode := circuits.MockBallotMode()
		ballotMode = &types.BallotMode{
			MaxCount:        uint8(mockMode.MaxCount.Uint64()),
			ForceUniqueness: mockMode.ForceUniqueness.Uint64() == 1,
			MaxValue:        (*types.BigInt)(mockMode.MaxValue),
			MinValue:        (*types.BigInt)(mockMode.MinValue),
			MaxTotalCost:    (*types.BigInt)(mockMode.MaxTotalCost),
			MinTotalCost:    (*types.BigInt)(mockMode.MinTotalCost),
			CostFromWeight:  mockMode.CostFromWeight.Uint64() == 1,
			CostExponent:    uint8(mockMode.CostExp.Uint64()),
		}

		pid, encryptionKey = createProcess(c, contracts, cli, root, *ballotMode)
		t.Logf("Process ID: %s", pid.String())
	})

	c.Run("create vote", func(c *qt.C) {
		for i := range signers {
			// generate a vote for the first participant
			vote := createVote(c, pid, encryptionKey, signers[i])
			// generate census proof for first participant
			censusProof := generateCensusProof(c, cli, root, signers[i].Address().Bytes())
			c.Assert(censusProof, qt.Not(qt.IsNil))
			c.Assert(censusProof.Siblings, qt.IsNotNil)
			vote.CensusProof = *censusProof
			_, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, 200)
			c.Logf("Vote %d created", i)
		}

		// wait to process the vote
		voteWaiter, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		done := false
		for {
			if done {
				break
			}
			select {
			case <-voteWaiter.Done():
				c.Fatal("timeout waiting for vote to be processed")
			default:
				if stg.CountVerifiedBallots(pid.Marshal()) == numBallots {
					break
				}
				time.Sleep(time.Second)
			}
		}
		t.Logf("All votes processed, waiting for aggregation")

		for {
			_, _, err := stg.NextBallotBatch(pid.Marshal())
			switch {
			case err == nil:
				log.Debug("aggregated ballot batch found")
				return
			case !errors.Is(err, storage.ErrNoMoreElements):
				c.Fatalf("unexpected error: %v", err)
			default:
				time.Sleep(time.Second)
			}
		}
	})
}
