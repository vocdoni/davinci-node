package state_test

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/crypto/blobs"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/spec"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/state"
	statetest "github.com/vocdoni/davinci-node/state/testutil"
	"github.com/vocdoni/davinci-node/types"
)

func TestBlobDataStructures(t *testing.T) {
	c := qt.New(t)

	// Create encryption key pair
	publicKey, _, err := elgamal.GenerateKey(state.Curve)
	c.Assert(err, qt.IsNil)

	// Initialize state
	state, err := state.New(memdb.New(), testutil.RandomProcessID())
	c.Assert(err, qt.IsNil)
	defer func() {
		if err := state.Close(); err != nil {
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to close state"))
		}
	}()

	// Initialize state with process parameters
	ballotMode := spec.BallotMode{
		NumFields:      3,
		GroupSize:      3,
		MaxValue:       100,
		MinValue:       0,
		MaxValueSum:    1000,
		MinValueSum:    0,
		CostExponent:   1,
		UniqueValues:   false,
		CostFromWeight: false,
	}
	ballotModeCircuit, err := ballotMode.Pack()
	c.Assert(err, qt.IsNil)
	err = state.Initialize(types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(), ballotModeCircuit, types.EncryptionKeyFromPoint(publicKey))
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize state"))

	// Create test votes
	votes := statetest.NewVotesForTest(publicKey, 3, 1)

	err = state.AddVotesBatch(votes)
	c.Assert(err, qt.IsNil, qt.Commentf("add votes batch"))

	// Test blob data serialization logic
	t.Run("TestBlobDataSerialization", func(t *testing.T) {
		c := qt.New(t)
		// Test the optimized data packing logic
		var cells [4096][32]byte
		cell := 0
		push := func(bi *big.Int) {
			c.Assert(cell < 4096, qt.IsTrue, qt.Commentf("blob overflow at cell %d", cell))
			bi.FillBytes(cells[cell][:]) // big-endian, left-padded
			cell++
		}

		// Pack results first
		for _, p := range state.NewResultsAdd().BigInts() {
			push(p)
		}
		for _, p := range state.NewResultsSub().BigInts() {
			push(p)
		}

		// Pack votes
		for _, v := range state.Votes() {
			push(v.VoteID.BigInt())                // voteID
			push(v.Address)                        // address
			for _, p := range v.Ballot.BigInts() { // ballot coords
				push(p)
			}
		}

		// Add sentinel
		push(big.NewInt(0))

		// Verify we used the expected number of cells
		coordsPerBallot := params.FieldsPerBallot * 4
		resultsCells := 2 * coordsPerBallot     // resultsAdd + resultsSub
		cellsPerVote := 1 + 1 + coordsPerBallot // voteID + address + ballot
		sentinelCells := 1
		expectedCells := resultsCells + len(votes)*cellsPerVote + sentinelCells
		c.Assert(cell, qt.Equals, expectedCells, qt.Commentf("Expected %d cells, used %d", expectedCells, cell))

		// Test that we can reconstruct the data using optimized parsing
		cellIndex := 0
		getCell := func() *big.Int {
			if cellIndex >= cell {
				return big.NewInt(0)
			}
			result := new(big.Int).SetBytes(cells[cellIndex][:])
			cellIndex++
			return result
		}

		// Verify results can be reconstructed first
		originalResultsAdd := state.NewResultsAdd().BigInts()
		for i, originalCoord := range originalResultsAdd {
			reconstructedCoord := getCell()
			c.Assert(originalCoord.Cmp(reconstructedCoord), qt.Equals, 0, qt.Commentf("ResultsAdd coordinate %d mismatch", i))
		}

		originalResultsSub := state.NewResultsSub().BigInts()
		for i, originalCoord := range originalResultsSub {
			reconstructedCoord := getCell()
			c.Assert(originalCoord.Cmp(reconstructedCoord), qt.Equals, 0, qt.Commentf("ResultsSub coordinate %d mismatch", i))
		}

		// Verify votes can be reconstructed
		for i, originalVote := range state.Votes() {
			voteID := getCell()
			address := getCell()

			// Verify vote ID and address
			c.Assert(originalVote.VoteID.BigInt().Cmp(voteID), qt.Equals, 0, qt.Commentf("Vote %d ID mismatch", i))
			c.Assert(originalVote.Address.Cmp(address), qt.Equals, 0, qt.Commentf("Vote %d address mismatch", i))

			// Verify ballot coordinates
			originalCoords := originalVote.Ballot.BigInts()
			for j, originalCoord := range originalCoords {
				reconstructedCoord := getCell()
				c.Assert(originalCoord.Cmp(reconstructedCoord), qt.Equals, 0, qt.Commentf("Vote %d ballot coordinate %d mismatch", i, j))
			}
		}

		// Verify sentinel
		sentinel := getCell()
		c.Assert(sentinel.Cmp(big.NewInt(0)), qt.Equals, 0, qt.Commentf("Expected sentinel (0), got %s", sentinel.String()))
	})
}

func TestBlobStateTransition(t *testing.T) {
	c := qt.New(t)
	processID := testutil.RandomProcessID()
	numTransitions := 5 // Test multiple state transitions

	// Create encryption key pair
	publicKey, _, err := elgamal.GenerateKey(state.Curve)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate encryption key"))

	// Initialize original state
	originalState, err := state.New(memdb.New(), processID)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to create original state"))
	defer func() {
		if err := originalState.Close(); err != nil {
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to close original state"))
		}
	}()

	// Initialize state with process parameters
	ballotMode := testutil.BallotMode()
	ballotModeCircuit, err := ballotMode.Pack()
	c.Assert(err, qt.IsNil)
	err = originalState.Initialize(types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(), ballotModeCircuit, types.EncryptionKeyFromPoint(publicKey))
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize original state"))

	// Store blobs and roots for each transition
	type TransitionData struct {
		Blob     *blobs.BlobEvalData
		Root     *big.Int
		Votes    []*state.Vote
		BatchNum uint64
	}
	transitions := make([]TransitionData, numTransitions)

	// Perform multiple state transitions
	for i := range numTransitions {
		batchNum := uint64(i + 1)

		// Create test votes for this transition (different votes each time)
		votes := statetest.NewVotesForTest(publicKey, 3, i)

		// Perform batch operation on original state
		err = originalState.AddVotesBatch(votes)
		c.Assert(err, qt.IsNil, qt.Commentf("add votes batch"))

		// Get state root after this transition
		root, err := originalState.RootAsBigInt()
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to get root for batch %d", i+1))

		// Generate blob with KZG commitment
		blob, err := originalState.BuildKZGCommitment()
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to build KZG commitment for batch %d", i+1))

		// Store transition data
		// Note: With EIP-7594, we now have cell proofs instead of a single blob proof.
		// Store the first cell proof for compatibility with the test structure.
		transitions[i] = TransitionData{
			Blob:     blob,
			Root:     root,
			Votes:    votes,
			BatchNum: batchNum,
		}
	}

	// Verify blob structure for first transition
	t.Run("VerifyBlobStructure_First", func(t *testing.T) {
		firstTransition := transitions[0]
		verifyBlobStructureBasic(t, firstTransition.Blob.Blob, firstTransition.Votes)
	})

	// Verify KZG commitment for first transition
	t.Run("VerifyKZGCommitment_First", func(t *testing.T) {
		firstTransition := transitions[0]
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to get tx sidecar for first transition"))
		verifyKZGCommitment(t, firstTransition.Blob.Blob, &firstTransition.Blob.Commitment,
			firstTransition.Blob.Z, firstTransition.Blob.Y, firstTransition.Blob.TxSidecar().BlobHashes()[0])
	})

	// Verify blob structure for last transition
	t.Run("VerifyBlobStructure_Last", func(t *testing.T) {
		lastTransition := transitions[numTransitions-1]
		verifyBlobStructureBasic(t, lastTransition.Blob.Blob, lastTransition.Votes)
	})

	// Verify KZG commitment for last transition
	t.Run("VerifyKZGCommitment_Last", func(t *testing.T) {
		lastTransition := transitions[numTransitions-1]
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to get tx sidecar for last transition"))
		verifyKZGCommitment(t, lastTransition.Blob.Blob, &lastTransition.Blob.Commitment,
			lastTransition.Blob.Z, lastTransition.Blob.Y, lastTransition.Blob.TxSidecar().BlobHashes()[0])
	})

	// Test state restoration for first transition
	t.Run("RestoreStateFromBlob_First", func(t *testing.T) {
		firstTransition := transitions[0]

		// Create a fresh state with only the initial setup
		testState, err := state.New(memdb.New(), processID)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create test state"))
		defer func() {
			if err := testState.Close(); err != nil {
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to close test state"))
			}
		}()
		err = testState.Initialize(types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(), ballotModeCircuit, types.EncryptionKeyFromPoint(publicKey))
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize test state"))

		// Apply first transition
		err = testState.AddVotesBatch(firstTransition.Votes)
		c.Assert(err, qt.IsNil, qt.Commentf("add votes batch"))

		expectedRoot, err := testState.RootAsBigInt()
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to get expected root"))

		// Now test restoration from blob
		restoreStateFromBlob(t, firstTransition.Blob.Blob, processID, ballotMode, publicKey, expectedRoot)
	})

	// Test state restoration by applying all blobs in sequence
	t.Run("RestoreStateFromBlob_Sequential", func(t *testing.T) {
		// Create a fresh state for sequential restoration
		testState, err := state.New(memdb.New(), processID)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create test state"))
		defer func() {
			if err := testState.Close(); err != nil {
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to close test state"))
			}
		}()

		err = testState.Initialize(types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(), ballotModeCircuit, types.EncryptionKeyFromPoint(publicKey))
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize test state"))

		// Apply each blob in sequence to restore the cumulative state
		for i, transition := range transitions {
			// Apply the blob data to the test state
			err = testState.ApplyBlobToState(transition.Blob.Blob)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to apply blob to state for transition %d", i+1))

			// Verify the state root matches the expected root for this transition
			restoredRoot, err := testState.RootAsBigInt()
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to get restored root for transition %d", i+1))

			c.Assert(transition.Root.Cmp(restoredRoot), qt.Equals, 0,
				qt.Commentf("Sequential restoration root mismatch for transition %d: expected %s, got %s",
					i+1, transition.Root.String(), restoredRoot.String()))
		}
	})

	// Test that individual transition blobs work correctly
	t.Run("RestoreStateFromBlob_LastTransitionOnly", func(t *testing.T) {
		lastTransition := transitions[numTransitions-1]

		// Create a state with all transitions except the last one
		testState, err := state.New(memdb.New(), processID)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create test state"))
		defer func() {
			if err := testState.Close(); err != nil {
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to close test state"))
			}
		}()

		err = testState.Initialize(types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(), ballotModeCircuit, types.EncryptionKeyFromPoint(publicKey))
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize test state"))

		// Apply all transitions except the last one
		for i := range numTransitions - 1 {
			err = testState.AddVotesBatch(transitions[i].Votes)
			c.Assert(err, qt.IsNil, qt.Commentf("add votes batch"))
		}

		// Now apply the last transition using the blob
		err = testState.ApplyBlobToState(lastTransition.Blob.Blob)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to apply last blob to state"))

		// Verify the final state root matches
		finalRoot, err := testState.RootAsBigInt()
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to get final root"))

		c.Assert(lastTransition.Root.Cmp(finalRoot), qt.Equals, 0,
			qt.Commentf("Final root mismatch: expected %s, got %s",
				lastTransition.Root.String(), finalRoot.String()))
	})

	// Verify that all transitions have different roots (state is actually changing)
	t.Run("VerifyUniqueRoots", func(t *testing.T) {
		for i := range numTransitions {
			for j := i + 1; j < numTransitions; j++ {
				c.Assert(transitions[i].Root.Cmp(transitions[j].Root), qt.Not(qt.Equals), 0,
					qt.Commentf("Transitions %d and %d have the same root, but should be different", i+1, j+1))
			}
		}
	})
}

func verifyBlobStructureBasic(t *testing.T, blob *types.Blob, votes []*state.Vote) {
	c := qt.New(t)
	// Parse blob data
	blobData, err := state.ParseBlobData(blob.Bytes())
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to parse blob data"))

	// Verify number of votes
	c.Assert(len(blobData.Votes), qt.Equals, len(votes), qt.Commentf("Expected %d votes, got %d", len(votes), len(blobData.Votes)))

	// Verify vote data
	for i, originalVote := range votes {
		if i >= len(blobData.Votes) {
			break
		}

		parsedVote := blobData.Votes[i]

		// Verify address
		c.Assert(originalVote.Address.Cmp(parsedVote.Address), qt.Equals, 0,
			qt.Commentf("Vote %d address mismatch: expected %s, got %s", i, originalVote.Address.String(), parsedVote.Address.String()))

		// Verify vote ID
		c.Assert(originalVote.VoteID, qt.Equals, parsedVote.VoteID, qt.Commentf("Vote %d ID mismatch", i))

		// Verify ballot coordinates match (comparing reencrypted ballot since that's what's stored in blob)
		originalCoords := originalVote.ReencryptedBallot.BigInts()

		c.Assert(len(originalCoords), qt.Equals, params.FieldsPerBallot*elgamal.BigIntsPerCiphertext,
			qt.Commentf("Vote %d ballot coordinate count mismatch", i))
	}

	// Verify that results data exists (but don't compare values since they accumulate across transitions)
	c.Assert(len(blobData.ResultsAdd), qt.Equals, 32, qt.Commentf("Expected 32 ResultsAdd coordinates, got %d", len(blobData.ResultsAdd)))
	c.Assert(len(blobData.ResultsSub), qt.Equals, 32, qt.Commentf("Expected 32 ResultsSub coordinates, got %d", len(blobData.ResultsSub)))
}

func verifyKZGCommitment(t *testing.T, blob *types.Blob, commit *types.KZGCommitment, z, y *big.Int, versionedHash [32]byte) {
	c := qt.New(t)
	// Verify commitment can be regenerated from blob
	recomputedCommit, err := blob.ComputeCommitment()
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to recompute commitment"))

	c.Assert(commit, qt.DeepEquals, &recomputedCommit, qt.Commentf("Commitment mismatch"))

	// Verify versioned hash format using the same method as the implementation
	expectedVersionedHash := commit.CalcBlobHashV1(sha256.New())

	c.Assert(versionedHash, qt.Equals, expectedVersionedHash, qt.Commentf("Versioned hash mismatch"))

	// Verify z is within BN254 scalar field
	bn254Modulus := new(big.Int)
	bn254Modulus.SetString("21888242871839275222246405745257275088548364400416034343698204186575808495617", 10)
	c.Assert(z.Cmp(bn254Modulus) < 0, qt.IsTrue, qt.Commentf("z value exceeds BN254 scalar field"))

	// Verify y value by computing point evaluation separately (just for verification)
	_, recomputedY, err := blob.ComputeProof(z)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to compute point evaluation"))

	c.Assert(y.Cmp(recomputedY), qt.Equals, 0, qt.Commentf("KZG evaluation (y value) mismatch"))
}

func restoreStateFromBlob(t *testing.T, blob *types.Blob, processID types.ProcessID, ballotMode spec.BallotMode, encryptionKey ecc.Point, expectedRoot *big.Int) {
	c := qt.New(t)
	// Parse blob data
	blobData, err := state.ParseBlobData(blob.Bytes())
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to parse blob data"))

	// Create new state
	newState, err := state.New(memdb.New(), processID)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to create new state"))
	defer func() {
		if err := newState.Close(); err != nil {
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to close new state"))
		}
	}()

	// Initialize new state with same parameters
	ballotModeCircuit, err := ballotMode.Pack()
	c.Assert(err, qt.IsNil)
	err = newState.Initialize(types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(), ballotModeCircuit, types.EncryptionKeyFromPoint(encryptionKey))
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize new state"))

	// Apply blob data to new state
	err = newState.ApplyBlobToState(blob)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to apply blob to state"))

	// Verify restored state root matches original
	restoredRoot, err := newState.RootAsBigInt()
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to get restored root"))

	c.Assert(expectedRoot.Cmp(restoredRoot), qt.Equals, 0,
		qt.Commentf("Restored state root mismatch: expected %s, got %s", expectedRoot.String(), restoredRoot.String()))

	// Verify individual votes can be retrieved
	for _, vote := range blobData.Votes {
		retrievedBallot, err := newState.EncryptedBallot(types.CalculateBallotIndex(vote.Address, types.IndexTODO))
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to retrieve ballot for address %s", vote.Address.String()))

		// Compare ballot coordinates
		retrievedCoords := retrievedBallot.BigInts()
		c.Assert(len(retrievedCoords), qt.Equals, params.FieldsPerBallot*elgamal.BigIntsPerCiphertext,
			qt.Commentf("Retrieved ballot coordinate count mismatch for address %s", vote.Address.String()))
	}

	// Verify results match
	restoredResultsAdd := newState.NewResultsAdd()
	restoredResultsSub := newState.NewResultsSub()

	if restoredResultsAdd != nil {
		restoredAddCoords := restoredResultsAdd.BigInts()
		c.Assert(len(blobData.ResultsAdd), qt.Equals, len(restoredAddCoords), qt.Commentf("Restored ResultsAdd coordinate count mismatch"))
		for i, coord := range blobData.ResultsAdd {
			c.Assert(coord.Cmp(restoredAddCoords[i]), qt.Equals, 0, qt.Commentf("Restored ResultsAdd coordinate %d mismatch", i))
		}
	}

	if restoredResultsSub != nil {
		restoredSubCoords := restoredResultsSub.BigInts()
		c.Assert(len(blobData.ResultsSub), qt.Equals, len(restoredSubCoords), qt.Commentf("Restored ResultsSub coordinate count mismatch"))
		for i, coord := range blobData.ResultsSub {
			c.Assert(coord.Cmp(restoredSubCoords[i]), qt.Equals, 0, qt.Commentf("Restored ResultsSub coordinate %d mismatch", i))
		}
	}
}

//go:embed testdata/blob.bin
var blobData string

func TestParseBlobData_FromFile(t *testing.T) {
	log.Init("debug", "stdout", nil)
	hexStr := strings.TrimSpace(blobData)
	hexStr = strings.TrimPrefix(hexStr, "0x")

	// remove whitespace/newlines just in case
	hexStr = strings.ReplaceAll(hexStr, "\n", "")
	hexStr = strings.ReplaceAll(hexStr, "\r", "")
	hexStr = strings.ReplaceAll(hexStr, " ", "")
	hexStr = strings.ReplaceAll(hexStr, "\t", "")

	raw, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatalf("hex decode failed: %v", err)
	}

	if len(raw) != types.BlobLength {
		t.Fatalf("unexpected blob length: got %d, want %d", len(raw), types.BlobLength)
	}

	data, err := state.ParseBlobData(raw)
	if err != nil {
		t.Fatalf("ParseBlobData returned error: %v", err)
	}

	// Basic sanity logs (helpful while debugging)
	t.Logf("parsed votes: %d", len(data.Votes))
	t.Logf("results add coords: %d", len(data.ResultsAdd))
	t.Logf("results sub coords: %d", len(data.ResultsSub))

	// Optional extra invariants:
	if len(data.ResultsAdd) != len(data.ResultsSub) {
		t.Fatalf("results length mismatch add=%d sub=%d", len(data.ResultsAdd), len(data.ResultsSub))
	}
}
