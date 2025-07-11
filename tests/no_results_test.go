package tests

import (
	"context"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/workers"
)

func TestNoResults(t *testing.T) {
	nVoters := 1000
	c := qt.New(t)
	testWorkerSeed := "test-seed"
	testWorkerTimeout := time.Second * 5

	// Setup
	ctx := t.Context()
	services := NewTestService(t, ctx, testWorkerSeed, testWorkerTimeout, workers.DefaultWorkerBanRules)
	_, port := services.API.HostPort()
	cli, err := NewTestClient(port)
	c.Assert(err, qt.IsNil)

	c.Run("create organization", func(c *qt.C) {
		orgAddr := createOrganization(c, services.Contracts)
		t.Logf("Organization address: %s", orgAddr.String())
	})

	bm := &types.BallotMode{
		MaxCount:        circuits.MockMaxCount,
		ForceUniqueness: circuits.MockForceUniqueness == 1,
		MaxValue:        new(types.BigInt).SetUint64(circuits.MockMaxValue),
		MinValue:        new(types.BigInt).SetUint64(circuits.MockMinValue),
		MaxTotalCost:    new(types.BigInt).SetUint64(circuits.MockMaxTotalCost),
		MinTotalCost:    new(types.BigInt).SetUint64(circuits.MockMinTotalCost),
		CostFromWeight:  circuits.MockCostFromWeight == 1,
		CostExponent:    circuits.MockCostExp,
	}

	var pid *types.ProcessID
	var censusRoot []byte
	var encryptionKey *types.EncryptionKey
	var stateRoot *types.HexBytes
	c.Run("create process", func(c *qt.C) {
		// Create census with numBallot participants
		censusRoot, _, _ = createCensus(c, cli, nVoters)

		// this final call is the good one, with the real censusRoot, should return the correct stateRoot and encryptionKey that
		// we'll use to create process in contracts
		pid, encryptionKey, stateRoot = createProcessInSequencer(c, services.Contracts, cli, censusRoot, bm)

		// now create process in contracts
		pid2 := createProcessInContracts(c, services.Contracts, censusRoot, bm, encryptionKey, stateRoot, time.Minute*2)
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

	time.Sleep(time.Minute)

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
			case <-ctx.Done():
				c.Fatalf("Timeout waiting for votes to be processed and published at contract")
			}
		}
	})
}
