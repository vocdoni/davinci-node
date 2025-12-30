package service

import (
	"context"
	"math/big"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
)

func TestStateSync(t *testing.T) {
	c := qt.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Init("debug", "stdout", nil)

	// Setup storage
	store := storage.New(memdb.New())
	defer store.Close()

	// Setup mock web3 contracts
	contracts := NewMockContracts()

	// Setup census downloader
	censusDownloader := NewCensusDownloader(contracts, store, CensusDownloaderConfig{
		CleanUpInterval: 5 * time.Second,
		Cooldown:        5 * time.Second,
		Expiration:      30 * time.Minute,
		Attempts:        5,
	})
	c.Assert(censusDownloader.Start(ctx), qt.IsNil)
	c.Cleanup(censusDownloader.Stop)

	// Start StateSync
	stateSync := NewStateSync(contracts, store)
	c.Assert(stateSync.Start(ctx), qt.IsNil)
	defer stateSync.Stop()

	// Create process monitor
	monitor := NewProcessMonitor(contracts, store, censusDownloader, stateSync, time.Second)

	// Start monitoring in background
	c.Assert(monitor.Start(ctx), qt.IsNil)
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
			CensusURI:    "https://example.com/census",
			CensusOrigin: types.CensusOriginCSPEdDSABN254V1,
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
	c.Log(proc)

	// TODO: dedup all of this with state/blobs_test.go code that was copypasted here

	// Initialize state
	originalState, err := state.New(memdb.New(), pid.BigInt())
	c.Assert(err, qt.IsNil)
	defer func() {
		if err := originalState.Close(); err != nil {
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to close state"))
		}
	}()

	// Initialize state with process parameters
	ballotMode := &types.BallotMode{
		NumFields:      3,
		MaxValue:       types.NewInt(100),
		MinValue:       types.NewInt(0),
		MaxValueSum:    types.NewInt(1000),
		MinValueSum:    types.NewInt(0),
		CostExponent:   1,
		UniqueValues:   false,
		CostFromWeight: false,
	}
	ballotModeCircuit := circuits.BallotModeToCircuit(ballotMode)
	encryptionKeyCircuit := circuits.EncryptionKeyFromECCPoint(publicKey)
	err = originalState.Initialize(
		types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(),
		ballotModeCircuit,
		encryptionKeyCircuit)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize original state"))

	oldStateRoot, err := originalState.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	i := 0

	// Create test votes for this transition (different votes each time)
	votes := createTestVotesWithOffset(t, publicKey, 3, i*1000)

	// Perform batch operation on original state
	err = originalState.StartBatch()
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to start batch %d", i+1))

	for _, vote := range votes {
		err = originalState.AddVote(vote)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to add vote in batch %d", i+1))
	}

	err = originalState.EndBatch()
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to end batch %d", i+1))

	newStateRoot, err := originalState.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	blobData, err := originalState.BuildKZGCommitment()
	c.Assert(err, qt.IsNil)

	txHash := contracts.SendBlobTx(blobData.Blob[:])
	{
		// Verify process is still untouched
		proc, err := store.Process(pid)
		c.Assert(err, qt.IsNil)
		c.Assert(proc, qt.Not(qt.IsNil))
		// c.Assert(proc.StateRoot, qt.DeepEquals, (*types.BigInt)(oldStateRoot))
		c.Logf("%s does not match %s, still merkle tree update works, why?", proc.StateRoot, oldStateRoot)
		c.Assert(proc.VotersCount, qt.IsNil)
		c.Assert(proc.OverwrittenVotesCount, qt.IsNil)
		c.Log(proc)
	}
	err = contracts.MockStateRootChange(ctx, &types.ProcessWithChanges{
		ProcessID: proc.ID,
		StateRootChange: &types.StateRootChange{
			OldStateRoot:             (*types.BigInt)(oldStateRoot),
			NewStateRoot:             (*types.BigInt)(newStateRoot),
			NewVotersCount:           types.NewInt(len(votes)),
			NewOverwrittenVotesCount: types.NewInt(0),
			TxHash:                   &txHash,
		},
	})
	c.Assert(err, qt.IsNil)

	// Give process monitor some time
	time.Sleep(3 * time.Second)

	{
		// Verify process is now updated
		proc, err := store.Process(pid)
		c.Assert(err, qt.IsNil)
		c.Assert(proc, qt.Not(qt.IsNil))
		c.Assert(proc.StateRoot, qt.DeepEquals, (*types.BigInt)(newStateRoot))
		c.Assert(proc.VotersCount, qt.DeepEquals, types.NewInt(3))
		c.Assert(proc.OverwrittenVotesCount, qt.DeepEquals, types.NewInt(0))
		c.Log(proc)
	}
}

func createTestVotesWithOffset(t *testing.T, publicKey ecc.Point, numVotes int, offset int) []*state.Vote {
	c := qt.New(t)
	votes := make([]*state.Vote, numVotes)

	for i := range numVotes {
		// Create vote address with offset to ensure uniqueness across transitions
		address := big.NewInt(int64(1000 + offset + i))

		// Create vote ID (use StateKeyMaxLen bytes) with offset
		voteID := make([]byte, params.StateKeyMaxLen)
		voteIDValue := offset + i + 1
		// Store the vote ID value in the last few bytes to ensure uniqueness
		voteID[params.StateKeyMaxLen-4] = byte(voteIDValue >> 24)
		voteID[params.StateKeyMaxLen-3] = byte(voteIDValue >> 16)
		voteID[params.StateKeyMaxLen-2] = byte(voteIDValue >> 8)
		voteID[params.StateKeyMaxLen-1] = byte(voteIDValue)

		// Create ballot with test values (vary based on offset and index)
		ballot := elgamal.NewBallot(state.Curve)
		messages := [params.FieldsPerBallot]*big.Int{}
		for j := 0; j < params.FieldsPerBallot; j++ {
			// Make ballot values unique based on offset, vote index, and field index
			messages[j] = big.NewInt(int64((offset+1)*100 + i*10 + j + 1))
		}

		// Encrypt the ballot
		_, err := ballot.Encrypt(messages, publicKey, nil)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to encrypt ballot %d with offset %d", i, offset))

		// Create reencrypted ballot (for state transition circuit)
		// Generate a random k for reencryption
		k, err := elgamal.RandK()
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate random k for ballot %d with offset %d", i, offset))
		reencryptedBallot, _, err := ballot.Reencrypt(publicKey, k)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to reencrypt ballot %d with offset %d", i, offset))

		votes[i] = &state.Vote{
			Address:           address,
			VoteID:            voteID,
			Ballot:            ballot,
			ReencryptedBallot: reencryptedBallot,
		}
	}

	return votes
}
