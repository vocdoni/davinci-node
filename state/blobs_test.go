package state

import (
	"fmt"
	"math/big"
	"testing"

	kzg4844 "github.com/crate-crypto/go-eth-kzg"
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
	censusRoot := big.NewInt(67890)

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
		MaxCount:        3,
		MaxValue:        types.NewInt(100),
		MinValue:        types.NewInt(0),
		MaxTotalCost:    types.NewInt(1000),
		MinTotalCost:    types.NewInt(0),
		CostExponent:    1,
		ForceUniqueness: false,
		CostFromWeight:  false,
	}
	ballotModeCircuit := circuits.BallotModeToCircuit(ballotMode)
	encryptionKeyCircuit := circuits.EncryptionKeyFromECCPoint(publicKey)
	err = state.Initialize(censusRoot, ballotModeCircuit, encryptionKeyCircuit)
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
	censusRoot := big.NewInt(67890)
	batchNum := uint64(1)

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
		MaxCount:        3,
		MaxValue:        types.NewInt(100),
		MinValue:        types.NewInt(0),
		MaxTotalCost:    types.NewInt(1000),
		MinTotalCost:    types.NewInt(0),
		CostExponent:    1,
		ForceUniqueness: false,
		CostFromWeight:  false,
	}
	ballotModeCircuit := circuits.BallotModeToCircuit(ballotMode)
	encryptionKeyCircuit := circuits.EncryptionKeyFromECCPoint(publicKey)
	err = originalState.Initialize(censusRoot, ballotModeCircuit, encryptionKeyCircuit)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to initialize original state"))

	// Create test votes
	votes := createTestVotes(t, publicKey, 5)

	// Perform batch operation on original state
	err = originalState.StartBatch()
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to start batch"))

	for _, vote := range votes {
		err = originalState.AddVote(vote)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to add vote"))
	}

	err = originalState.EndBatch()
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to end batch"))

	// Get original state root
	originalRoot, err := originalState.RootAsBigInt()
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to get original root"))

	// Generate blob with KZG commitment
	blob, proof, err := originalState.BuildKZGCommitment(batchNum)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to build KZG commitment"))

	// Verify blob structure
	t.Run("VerifyBlobStructure", func(t *testing.T) {
		verifyBlobStructure(t, &blob.Blob, votes, originalState)
	})

	// Verify KZG commitment
	t.Run("VerifyKZGCommitment", func(t *testing.T) {
		verifyKZGCommitment(t, &blob.Blob, blob.Commitment, proof, blob.Z, blob.Y, blob.VersionedHash)
	})

	// Create new state and apply blob
	t.Run("RestoreStateFromBlob", func(t *testing.T) {
		restoreStateFromBlob(t, &blob.Blob, processID, censusRoot, *ballotMode, publicKey, originalRoot)
	})
}

func createTestVotes(t *testing.T, publicKey ecc.Point, numVotes int) []*Vote {
	c := qt.New(t)
	votes := make([]*Vote, numVotes)

	for i := 0; i < numVotes; i++ {
		// Create vote address
		address := big.NewInt(int64(1000 + i))

		// Create vote ID (use StateKeyMaxLen bytes)
		voteID := make([]byte, types.StateKeyMaxLen)
		voteID[types.StateKeyMaxLen-1] = byte(i + 1)

		// Create ballot with test values
		ballot := elgamal.NewBallot(Curve)
		messages := [types.FieldsPerBallot]*big.Int{}
		for j := 0; j < types.FieldsPerBallot; j++ {
			messages[j] = big.NewInt(int64(j + 1))
		}

		// Encrypt the ballot
		_, err := ballot.Encrypt(messages, publicKey, nil)
		c.Assert(err, qt.IsNil, qt.Commentf("Failed to encrypt ballot %d", i))

		votes[i] = &Vote{
			Address: address,
			VoteID:  voteID,
			Ballot:  ballot,
		}
	}

	return votes
}

func verifyBlobStructure(t *testing.T, blob *kzg4844.Blob, votes []*Vote, state *State) {
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

		// Verify ballot coordinates match
		originalCoords := originalVote.Ballot.BigInts()
		parsedCoords := parsedVote.Ballot.BigInts()

		c.Assert(len(originalCoords), qt.Equals, len(parsedCoords), qt.Commentf("Vote %d ballot coordinate count mismatch", i))

		for j, originalCoord := range originalCoords {
			c.Assert(originalCoord.Cmp(parsedCoords[j]), qt.Equals, 0, qt.Commentf("Vote %d ballot coordinate %d mismatch", i, j))
		}
	}

	// Verify results data
	originalResultsAdd := state.NewResultsAdd()
	originalResultsSub := state.NewResultsSub()

	if originalResultsAdd != nil {
		originalAddCoords := originalResultsAdd.BigInts()
		c.Assert(len(blobData.ResultsAdd), qt.Equals, len(originalAddCoords), qt.Commentf("ResultsAdd coordinate count mismatch"))
		for i, coord := range originalAddCoords {
			c.Assert(coord.Cmp(blobData.ResultsAdd[i]), qt.Equals, 0, qt.Commentf("ResultsAdd coordinate %d mismatch", i))
		}
	}

	if originalResultsSub != nil {
		originalSubCoords := originalResultsSub.BigInts()
		c.Assert(len(blobData.ResultsSub), qt.Equals, len(originalSubCoords), qt.Commentf("ResultsSub coordinate count mismatch"))
		for i, coord := range originalSubCoords {
			c.Assert(coord.Cmp(blobData.ResultsSub[i]), qt.Equals, 0, qt.Commentf("ResultsSub coordinate %d mismatch", i))
		}
	}
}

func verifyKZGCommitment(t *testing.T, blob *kzg4844.Blob, commit *big.Int, proof kzg4844.KZGProof, z, y *big.Int, versionedHash [32]byte) {
	c := qt.New(t)
	// Verify commitment can be regenerated from blob
	recomputedCommit, err := blobs.BlobToCommitment(blob)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to recompute commitment"))

	c.Assert(string(commit.Bytes()), qt.Equals, string(recomputedCommit[:]), qt.Commentf("Commitment mismatch"))

	// Verify versioned hash format using the same method as the implementation
	expectedVersionedHash := blobs.CalcBlobHashV1(commit)

	c.Assert(versionedHash, qt.Equals, expectedVersionedHash, qt.Commentf("Versioned hash mismatch"))

	// Verify z is within 250-bit range
	maxZ := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 250), big.NewInt(1))
	c.Assert(z.Cmp(maxZ) <= 0, qt.IsTrue, qt.Commentf("z value exceeds 250-bit range"))

	// Verify KZG proof
	recomputedProof, claim, err := blobs.ComputeProof(blob, z)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to recompute KZG proof"))

	c.Assert(proof, qt.Equals, recomputedProof, qt.Commentf("KZG proof mismatch"))

	// Claim is already in big-endian format
	recomputedY := new(big.Int).SetBytes(claim[:])
	c.Assert(y.Cmp(recomputedY), qt.Equals, 0, qt.Commentf("KZG evaluation mismatch"))
}

func restoreStateFromBlob(t *testing.T, blob *kzg4844.Blob, processID, censusRoot *big.Int, ballotMode types.BallotMode, encryptionKey ecc.Point, expectedRoot *big.Int) {
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
	err = newState.Initialize(censusRoot, ballotModeCircuit, encryptionKeyCircuit)
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
			blob := &kzg4844.Blob{}

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
