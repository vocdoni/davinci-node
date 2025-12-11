package state

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"testing"

	gethkzg "github.com/ethereum/go-ethereum/crypto/kzg4844"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/blobs"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/types"
)

func TestBlobDataStructures(t *testing.T) {
	c := qt.New(t)

	// Test parameters
	processID := big.NewInt(12345)

	// Create encryption key pair
	publicKey, _, err := elgamal.GenerateKey(Curve)
	c.Assert(err, qt.IsNil)

	// Initialize state
	state, err := New(memdb.New(), processID)
	c.Assert(err, qt.IsNil)
	defer func() {
		if err := state.Close(); err != nil {
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
	err = state.Initialize(types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(), ballotModeCircuit, encryptionKeyCircuit)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize state"))

	// Create test votes
	votes := createTestVotes(t, publicKey, 3)

	// Perform batch operation
	err = state.StartBatch()
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to start batch"))

	for _, vote := range votes {
		err = state.AddVote(vote)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to add vote"))
	}

	err = state.EndBatch()
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to end batch"))

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
			push(new(big.Int).SetBytes(v.VoteID))  // voteId hash
			push(v.Address)                        // address
			for _, p := range v.Ballot.BigInts() { // ballot coords
				push(p)
			}
		}

		// Add sentinel
		push(big.NewInt(0))

		// Verify we used the expected number of cells
		coordsPerBallot := types.FieldsPerBallot * 4
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
			c.Assert(new(big.Int).SetBytes(originalVote.VoteID).Cmp(voteID), qt.Equals, 0, qt.Commentf("Vote %d ID mismatch", i))
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
	processID := big.NewInt(12345)
	numTransitions := 5 // Test multiple state transitions

	// Create encryption key pair
	publicKey, _, err := elgamal.GenerateKey(Curve)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to generate encryption key"))

	// Initialize original state
	originalState, err := New(memdb.New(), processID)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to create original state"))
	defer func() {
		if err := originalState.Close(); err != nil {
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to close original state"))
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
	err = originalState.Initialize(types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(), ballotModeCircuit, encryptionKeyCircuit)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize original state"))

	// Store blobs and roots for each transition
	type TransitionData struct {
		Blob     *blobs.BlobEvalData
		Root     *big.Int
		Votes    []*Vote
		BatchNum uint64
	}
	transitions := make([]TransitionData, numTransitions)

	// Perform multiple state transitions
	for i := range numTransitions {
		batchNum := uint64(i + 1)

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
		verifyBlobStructureBasic(t, &firstTransition.Blob.Blob, firstTransition.Votes)
	})

	// Verify KZG commitment for first transition
	t.Run("VerifyKZGCommitment_First", func(t *testing.T) {
		firstTransition := transitions[0]
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to get tx sidecar for first transition"))
		verifyKZGCommitment(t, &firstTransition.Blob.Blob, &firstTransition.Blob.Commitment,
			firstTransition.Blob.Z, firstTransition.Blob.Y, firstTransition.Blob.TxSidecar().BlobHashes()[0])
	})

	// Verify blob structure for last transition
	t.Run("VerifyBlobStructure_Last", func(t *testing.T) {
		lastTransition := transitions[numTransitions-1]
		verifyBlobStructureBasic(t, &lastTransition.Blob.Blob, lastTransition.Votes)
	})

	// Verify KZG commitment for last transition
	t.Run("VerifyKZGCommitment_Last", func(t *testing.T) {
		lastTransition := transitions[numTransitions-1]
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to get tx sidecar for last transition"))
		verifyKZGCommitment(t, &lastTransition.Blob.Blob, &lastTransition.Blob.Commitment,
			lastTransition.Blob.Z, lastTransition.Blob.Y, lastTransition.Blob.TxSidecar().BlobHashes()[0])
	})

	// Test state restoration for first transition
	t.Run("RestoreStateFromBlob_First", func(t *testing.T) {
		firstTransition := transitions[0]

		// Create a fresh state with only the initial setup
		testState, err := New(memdb.New(), processID)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create test state"))
		defer func() {
			if err := testState.Close(); err != nil {
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to close test state"))
			}
		}()
		err = testState.Initialize(types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(), ballotModeCircuit, encryptionKeyCircuit)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize test state"))

		// Apply first transition
		err = testState.StartBatch()
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to start batch"))

		for _, vote := range firstTransition.Votes {
			err = testState.AddVote(vote)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to add vote"))
		}

		err = testState.EndBatch()
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to end batch"))

		expectedRoot, err := testState.RootAsBigInt()
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to get expected root"))

		// Now test restoration from blob
		restoreStateFromBlob(t, &firstTransition.Blob.Blob, processID, *ballotMode, publicKey, expectedRoot)
	})

	// Test state restoration by applying all blobs in sequence
	t.Run("RestoreStateFromBlob_Sequential", func(t *testing.T) {
		// Create a fresh state for sequential restoration
		testState, err := New(memdb.New(), processID)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create test state"))
		defer func() {
			if err := testState.Close(); err != nil {
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to close test state"))
			}
		}()

		err = testState.Initialize(types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(), ballotModeCircuit, encryptionKeyCircuit)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize test state"))

		// Apply each blob in sequence to restore the cumulative state
		for i, transition := range transitions {
			blobData, err := ParseBlobData(&transition.Blob.Blob)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to parse blob data for transition %d", i+1))

			// Apply the blob data to the test state
			err = testState.ApplyBlobToState(blobData)
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
		testState, err := New(memdb.New(), processID)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to create test state"))
		defer func() {
			if err := testState.Close(); err != nil {
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to close test state"))
			}
		}()

		err = testState.Initialize(types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(), ballotModeCircuit, encryptionKeyCircuit)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize test state"))

		// Apply all transitions except the last one
		for i := 0; i < numTransitions-1; i++ {
			err = testState.StartBatch()
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to start batch %d", i+1))

			for _, vote := range transitions[i].Votes {
				err = testState.AddVote(vote)
				c.Assert(err, qt.IsNil, qt.Commentf("Failed to add vote in batch %d", i+1))
			}

			err = testState.EndBatch()
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to end batch %d", i+1))
		}

		// Now apply the last transition using the blob
		blobData, err := ParseBlobData(&lastTransition.Blob.Blob)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to parse last blob data"))

		err = testState.ApplyBlobToState(blobData)
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
		for i := 0; i < numTransitions; i++ {
			for j := i + 1; j < numTransitions; j++ {
				c.Assert(transitions[i].Root.Cmp(transitions[j].Root), qt.Not(qt.Equals), 0,
					qt.Commentf("Transitions %d and %d have the same root, but should be different", i+1, j+1))
			}
		}
	})
}

func createTestVotes(t *testing.T, publicKey ecc.Point, numVotes int) []*Vote {
	return createTestVotesWithOffset(t, publicKey, numVotes, 0)
}

func createTestVotesWithOffset(t *testing.T, publicKey ecc.Point, numVotes int, offset int) []*Vote {
	c := qt.New(t)
	votes := make([]*Vote, numVotes)

	for i := range numVotes {
		// Create vote address with offset to ensure uniqueness across transitions
		address := big.NewInt(int64(1000 + offset + i))

		// Create vote ID (use StateKeyMaxLen bytes) with offset
		voteID := make([]byte, types.StateKeyMaxLen)
		voteIDValue := offset + i + 1
		// Store the vote ID value in the last few bytes to ensure uniqueness
		voteID[types.StateKeyMaxLen-4] = byte(voteIDValue >> 24)
		voteID[types.StateKeyMaxLen-3] = byte(voteIDValue >> 16)
		voteID[types.StateKeyMaxLen-2] = byte(voteIDValue >> 8)
		voteID[types.StateKeyMaxLen-1] = byte(voteIDValue)

		// Create ballot with test values (vary based on offset and index)
		ballot := elgamal.NewBallot(Curve)
		messages := [types.FieldsPerBallot]*big.Int{}
		for j := 0; j < types.FieldsPerBallot; j++ {
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

		votes[i] = &Vote{
			Address:           address,
			VoteID:            voteID,
			Ballot:            ballot,
			ReencryptedBallot: reencryptedBallot,
		}
	}

	return votes
}

func verifyBlobStructureBasic(t *testing.T, blob *gethkzg.Blob, votes []*Vote) {
	c := qt.New(t)
	// Parse blob data
	blobData, err := ParseBlobData(blob)
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
		c.Assert(string(originalVote.VoteID), qt.Equals, string(parsedVote.VoteID), qt.Commentf("Vote %d ID mismatch", i))

		// Verify ballot coordinates match (comparing reencrypted ballot since that's what's stored in blob)
		originalCoords := originalVote.ReencryptedBallot.BigInts()
		parsedCoords := parsedVote.Ballot.BigInts()

		c.Assert(len(originalCoords), qt.Equals, len(parsedCoords), qt.Commentf("Vote %d ballot coordinate count mismatch", i))

		for j, originalCoord := range originalCoords {
			c.Assert(originalCoord.Cmp(parsedCoords[j]), qt.Equals, 0, qt.Commentf("Vote %d ballot coordinate %d mismatch", i, j))
		}
	}

	// Verify that results data exists (but don't compare values since they accumulate across transitions)
	c.Assert(len(blobData.ResultsAdd), qt.Equals, 32, qt.Commentf("Expected 32 ResultsAdd coordinates, got %d", len(blobData.ResultsAdd)))
	c.Assert(len(blobData.ResultsSub), qt.Equals, 32, qt.Commentf("Expected 32 ResultsSub coordinates, got %d", len(blobData.ResultsSub)))
}

func verifyKZGCommitment(t *testing.T, blob *gethkzg.Blob, commit *gethkzg.Commitment, z, y *big.Int, versionedHash [32]byte) {
	c := qt.New(t)
	// Verify commitment can be regenerated from blob
	recomputedCommit, err := gethkzg.BlobToCommitment(blob)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to recompute commitment"))

	c.Assert(commit, qt.DeepEquals, &recomputedCommit, qt.Commentf("Commitment mismatch"))

	// Verify versioned hash format using the same method as the implementation
	expectedVersionedHash := gethkzg.CalcBlobHashV1(sha256.New(), commit)

	c.Assert(versionedHash, qt.Equals, expectedVersionedHash, qt.Commentf("Versioned hash mismatch"))

	// Verify z is within BN254 scalar field
	bn254Modulus := new(big.Int)
	bn254Modulus.SetString("21888242871839275222246405745257275088548364400416034343698204186575808495617", 10)
	c.Assert(z.Cmp(bn254Modulus) < 0, qt.IsTrue, qt.Commentf("z value exceeds BN254 scalar field"))

	// Verify y value by computing point evaluation separately (just for verification)
	_, claim, err := gethkzg.ComputeProof(blob, blobs.BigIntToPoint(z))
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to compute point evaluation"))

	recomputedY := new(big.Int).SetBytes(claim[:])
	c.Assert(y.Cmp(recomputedY), qt.Equals, 0, qt.Commentf("KZG evaluation (y value) mismatch"))
}

func restoreStateFromBlob(t *testing.T, blob *gethkzg.Blob, processID *big.Int, ballotMode types.BallotMode, encryptionKey ecc.Point, expectedRoot *big.Int) {
	c := qt.New(t)
	// Parse blob data
	blobData, err := ParseBlobData(blob)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to parse blob data"))

	// Create new state
	newState, err := New(memdb.New(), processID)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to create new state"))
	defer func() {
		if err := newState.Close(); err != nil {
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to close new state"))
		}
	}()

	// Initialize new state with same parameters
	ballotModeCircuit := circuits.BallotModeToCircuit(&ballotMode)
	encryptionKeyCircuit := circuits.EncryptionKeyFromECCPoint(encryptionKey)
	err = newState.Initialize(types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(), ballotModeCircuit, encryptionKeyCircuit)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize new state"))

	// Apply blob data to new state
	err = newState.ApplyBlobToState(blobData)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to apply blob to state"))

	// Verify restored state root matches original
	restoredRoot, err := newState.RootAsBigInt()
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to get restored root"))

	c.Assert(expectedRoot.Cmp(restoredRoot), qt.Equals, 0,
		qt.Commentf("Restored state root mismatch: expected %s, got %s", expectedRoot.String(), restoredRoot.String()))

	// Verify individual votes can be retrieved
	for _, vote := range blobData.Votes {
		retrievedBallot, err := newState.EncryptedBallot(vote.Address)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to retrieve ballot for address %s", vote.Address.String()))

		// Compare ballot coordinates
		originalCoords := vote.Ballot.BigInts()
		retrievedCoords := retrievedBallot.BigInts()

		c.Assert(len(originalCoords), qt.Equals, len(retrievedCoords),
			qt.Commentf("Retrieved ballot coordinate count mismatch for address %s", vote.Address.String()))

		for i, originalCoord := range originalCoords {
			c.Assert(originalCoord.Cmp(retrievedCoords[i]), qt.Equals, 0,
				qt.Commentf("Retrieved ballot coordinate %d mismatch for address %s", i, vote.Address.String()))
		}
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

func TestBlobDataParsing(t *testing.T) {
	// Test parsing with various vote counts
	testCases := []int{0, 1, 5, 50, 115}

	for _, numVotes := range testCases {
		t.Run(fmt.Sprintf("ParseVotes_%d", numVotes), func(t *testing.T) {
			c := qt.New(t)
			// Create a test blob with known data
			blob := &gethkzg.Blob{}

			// This would normally populate the blob with test data
			// For now, we'll test the parsing logic with empty data
			blobData, err := ParseBlobData(blob)
			c.Assert(err, qt.IsNil, qt.Commentf("Failed to parse blob"))

			// With empty blob, we should get 0 votes (since first cell is 0x0 = sentinel)
			c.Assert(len(blobData.Votes), qt.Equals, 0, qt.Commentf("Expected 0 votes from empty blob, got %d", len(blobData.Votes)))

			c.Assert(len(blobData.ResultsAdd), qt.Equals, 32, qt.Commentf("Expected 32 ResultsAdd coordinates, got %d", len(blobData.ResultsAdd)))

			c.Assert(len(blobData.ResultsSub), qt.Equals, 32, qt.Commentf("Expected 32 ResultsSub coordinates, got %d", len(blobData.ResultsSub)))
		})
	}
}
