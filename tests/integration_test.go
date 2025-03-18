package tests

import (
	"context"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/vocdoni-z-sandbox/api"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/ballotproof"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/voteverifier"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ethereum"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/service"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

func init() {
	log.Init(log.LogLevelDebug, "stdout", nil)
	if err := service.DownloadArtifacts(30 * time.Minute); err != nil {
		log.Errorw(err, "failed to download artifacts")
	}
}

func TestIntegration(t *testing.T) {
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
		// Create census with 10 participants
		root, participants, signers = createCensus(c, cli, 10)

		// Generate proof for first participant
		proofs = make([]*types.CensusProof, 10)
		for i := range participants {
			proofs[i] = generateCensusProof(c, cli, root, participants[i].Key)
			c.Assert(proofs[i], qt.Not(qt.IsNil))
			c.Assert(proofs[i].Siblings, qt.IsNotNil)
		}
		// Check the first proof key is the same as the participant key and signer address
		qt.Assert(t, proofs[0].Key.String(), qt.DeepEquals, participants[0].Key.String())
		qt.Assert(t, string(proofs[0].Key), qt.DeepEquals, string(signers[0].Address().Bytes()))

		ballotMode = &types.BallotMode{
			MaxCount:        1,
			MaxValue:        new(types.BigInt).SetUint64(2),
			MinValue:        new(types.BigInt).SetUint64(0),
			ForceUniqueness: false,
			CostFromWeight:  false,
			CostExponent:    1,
			MaxTotalCost:    new(types.BigInt).SetUint64(2),
			MinTotalCost:    new(types.BigInt).SetUint64(0),
		}

		pid, encryptionKey = createProcess(c, contracts, cli, root, *ballotMode)
		t.Logf("Process ID: %s", pid.String())
	})

	c.Run("create vote", func(c *qt.C) {
		// load ballot proof artifacts
		c.Assert(ballotproof.Artifacts.LoadAll(), qt.IsNil)
		c.Assert(voteverifier.Artifacts.LoadAll(), qt.IsNil)

		// generate a vote for the first participant
		vote := createVote(c, pid, encryptionKey, signers[0])
		// generate census proof for first participant
		censusProof := generateCensusProof(c, cli, root, signers[0].Address().Bytes())
		c.Assert(censusProof, qt.Not(qt.IsNil))
		c.Assert(censusProof.Siblings, qt.IsNotNil)
		vote.CensusProof = *censusProof

		body, status, err := cli.Request("POST", vote, nil, api.VotesEndpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, 200)
		c.Log("Vote created", string(body))

		// wait to process the vote
		voteWaiter, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		for {
			select {
			case <-voteWaiter.Done():
				c.Fatal("timeout waiting for vote to be processed")
			default:
				if stg.CountVerifiedBallots(pid.Marshal()) == 1 {
					return
				}
				time.Sleep(time.Second)
			}
		}
	})
}
