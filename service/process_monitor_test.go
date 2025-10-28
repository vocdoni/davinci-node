package service

import (
	"bytes"
	"context"
	"testing"
	"time"

	eth2deneb "github.com/attestantio/go-eth2-client/spec/deneb"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

func TestProcessMonitor(t *testing.T) {
	c := qt.New(t)

	log.Init("debug", "stdout", nil)

	// Setup storage
	store := storage.New(memdb.New())
	defer store.Close()

	// Setup mock web3 contracts
	contracts := NewMockContracts()

	// Start monitoring in background
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start StateSync
	stateSync := NewStateSync(contracts, store)
	err := stateSync.Start(ctx)
	c.Assert(err, qt.IsNil)
	defer stateSync.Stop()

	// Create process monitor
	monitor := NewProcessMonitor(contracts, store, time.Second, stateSync)

	err = monitor.Start(ctx)
	c.Assert(err, qt.IsNil)
	defer monitor.Stop()

	// Create a new encryption key for the process
	publicKey, privateKey, err := elgamal.GenerateKey(curves.New(bjj.CurveType))
	c.Assert(err, qt.IsNil)

	// Create a new process
	pid, createTx, err := contracts.CreateProcess(&types.Process{
		Status:         types.ProcessStatusReady,
		OrganizationId: contracts.AccountAddress(),
		StateRoot:      new(types.BigInt).SetUint64(100),
		StartTime:      time.Now().Add(5 * time.Minute),
		Duration:       time.Hour,
		MetadataURI:    "https://example.com/metadata",
		BallotMode: &types.BallotMode{
			NumFields:      2,
			MaxValue:       new(types.BigInt).SetUint64(100),
			MinValue:       new(types.BigInt).SetUint64(0),
			MaxValueSum:    new(types.BigInt).SetUint64(0),
			MinValueSum:    new(types.BigInt).SetUint64(0),
			UniqueValues:   false,
			CostFromWeight: false,
		},
		Census: &types.Census{
			CensusRoot:   make([]byte, 32),
			MaxVotes:     new(types.BigInt).SetUint64(100),
			CensusURI:    "https://example.com/census",
			CensusOrigin: types.CensusOriginMerkleTree,
		},
	})
	c.Assert(err, qt.IsNil)
	c.Assert(createTx, qt.Not(qt.IsNil))

	// Store the encryption keys for the process pid
	err = store.SetEncryptionKeys(pid, publicKey, privateKey)
	c.Assert(err, qt.IsNil)

	// Wait for transaction to be mined
	err = contracts.WaitTxByHash(*createTx, 30*time.Second)
	c.Assert(err, qt.IsNil)

	// Give monitor time to detect and store the process
	time.Sleep(3 * time.Second)

	// Verify process was stored
	proc, err := store.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(proc, qt.Not(qt.IsNil))
	c.Assert(proc.MetadataURI, qt.Equals, "https://example.com/metadata")

	blob := &eth2deneb.Blob{}
	copy(blob[:], bytes.Repeat([]byte("MockBlob"), 131072))
	txHash := contracts.SendBlobTx(blob)
	err = contracts.MockStateRootChange(ctx, &types.ProcessWithStateRootChange{
		Process:                 proc,
		NewStateRoot:            types.NewInt(12345),
		NewVoteCount:            types.NewInt(2),
		NewVoteOverwrittenCount: types.NewInt(1),
		TxHash:                  txHash,
	})
	c.Assert(err, qt.IsNil)

	// Give monitor some time
	time.Sleep(3 * time.Second)
}
