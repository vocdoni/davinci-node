package tests

import (
	"context"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/client"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/types"
)

func setupProcess(c *qt.C, ctx context.Context, chainID uint64, origin types.CensusOrigin, censusVoters int, maxVoters int) *client.ProcessConfig {
	c.Helper()

	processConfig := &client.ProcessConfig{
		ChainID:    chainID,
		MaxVoters:  maxVoters,
		BallotMode: testutil.BallotMode(),
		VotersConfig: client.VotersConfig{
			NumVoters:     censusVoters,
			DefaultWeight: types.NewInt(testutil.Weight),
		},
		CensusConfig: client.CensusConfig{
			CensusOrigin: origin.String(),
		},
		CSPConfig: client.CSPConfig{
			CensusOrigin: origin.String(),
			Seed:         []byte(LocalCSPSeed),
		},
	}

	c.Run("create process", func(c *qt.C) {
		var err error
		processConfig.ProcessID, err = services.SequencerClient.CreateProcess(processConfig)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))

		if err := client.WaitUntilCondition(ctx, time.Second, func() (bool, error) {
			process, err := services.SequencerClient.OnChainProcess(processConfig.ProcessID)
			if err != nil {
				return false, err
			}
			return process.IsAcceptingVotes(), nil
		}); err != nil {
			c.Errorf("process is not accepting votes: %v", err)
			c.FailNow()
		}
	})

	return processConfig
}
