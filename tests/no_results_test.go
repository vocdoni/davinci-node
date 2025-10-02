package tests

import (
	"context"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/sequencer"
	"github.com/vocdoni/davinci-node/types"
)

func TestNoResults(t *testing.T) {
	if !boolEnvVar("EXTENDED_CI_TESTS") {
		t.Skip("Skipping worker integration test, set EXTENDED_CI_TESTS=1 to run it")
	}

	c := qt.New(t)

	// Set debug prover to catch circuit errors
	services.Sequencer.SetProver(sequencer.NewDebugProver(t))

	// Setup
	ctx := t.Context()

	_, port := services.API.HostPort()
	cli, err := NewTestClient(port)
	c.Assert(err, qt.IsNil)

	var (
		pid           *types.ProcessID
		encryptionKey *types.EncryptionKey
		ballotMode    *types.BallotMode
		censusRoot    []byte
	)

	c.Run("create process", func(c *qt.C) {
		// Create census with numBallot participants
		censusRoot, _, _, err = createCensus(cli, 10)
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

		// this final call is the good one, with the real censusRoot, should return the correct stateRoot and encryptionKey that
		// we'll use to create process in contracts
		var stateRoot *types.HexBytes
		pid, encryptionKey, stateRoot, err = createProcessInSequencer(services.Contracts, cli, testCensusOrigin(), censusRoot, ballotMode)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in sequencer"))

		// now create process in contracts
		pid2, err := createProcessInContracts(services.Contracts, testCensusOrigin(), censusRoot, ballotMode, encryptionKey, stateRoot)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create process in contracts"))
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

	t.Log("Waiting for process to start...")
	err = waitUntilProcessStarts(services.Contracts, pid, 2*time.Minute)
	c.Assert(err, qt.IsNil, qt.Commentf("Process did not reach Ready status in time"))

	var timeoutCh <-chan time.Time
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
