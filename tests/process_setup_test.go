package tests

import (
	"context"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/tests/helpers"
	"github.com/vocdoni/davinci-node/types"
)

type processSetup struct {
	pid           types.ProcessID
	encryptionKey *types.EncryptionKey
	signers       []*ethereum.Signer
	stateRoot     types.HexBytes
}

func setupProcess(c *qt.C, t *testing.T, globalCtx context.Context, origin types.CensusOrigin, censusVoters int, maxVoters int) processSetup {
	t.Helper()

	var setup processSetup

	c.Run("create process", func(c *qt.C) {
		censusCtx, cancel := context.WithCancel(t.Context())
		defer cancel()

		censusRoot, censusURI, signers, err := helpers.NewCensusWithRandomVoters(censusCtx, origin, censusVoters)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create census"))
		c.Assert(signers, qt.HasLen, censusVoters)
		setup.signers = signers

		setup.pid, setup.encryptionKey, err = helpers.NewProcess(services.Contracts, services.HTTPClient)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))

		onchainPID, err := helpers.NewProcessOnChain(services.Contracts, origin, censusURI, censusRoot, defaultBallotMode, setup.encryptionKey, maxVoters)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in contracts"))
		c.Assert(onchainPID.String(), qt.Equals, setup.pid.String())

		if err := helpers.WaitUntilCondition(globalCtx, 200*time.Millisecond, func() bool {
			process, err := services.Storage.Process(setup.pid)
			if err != nil {
				return false
			}
			return process.IsAcceptingVotes()
		}); err != nil {
			c.Fatal("Timeout waiting for process to be created in storage")
			c.FailNow()
		}

		process, err := services.Storage.Process(setup.pid)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to get process from storage"))
		c.Assert(process.StateRoot, qt.Not(qt.IsNil), qt.Commentf("Process state root is nil"))
		setup.stateRoot = process.StateRoot.Bytes()
		t.Logf("Process ID: %s", setup.pid.String())

		if err := helpers.WaitUntilCondition(globalCtx, 200*time.Millisecond, func() bool {
			return services.Sequencer.ExistsProcessID(setup.pid)
		}); err != nil {
			c.Fatal("Timeout waiting for process to be registered in sequencer")
			c.FailNow()
		}
	})

	return setup
}
