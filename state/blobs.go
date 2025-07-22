package state

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"unsafe"

	bn254 "github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/hash/poseidon"
	"github.com/vocdoni/davinci-node/types"
)

// FieldElementsPerBlob defines the number of field elements per blob
const FieldElementsPerBlob = 4096

// BytesPerFieldElement defines the number of bytes per field element
const BytesPerFieldElement = 32

// BN254 scalar-field modulus
var pBN = bn254.ID.ScalarField()

// BlobData represents the structured data extracted from a blob
type BlobData struct {
	Votes      []*Vote
	ResultsAdd []*big.Int
	ResultsSub []*big.Int
}

// ComputeEvaluationPoint computes evaluation point z using Poseidon hash.
// z = PoseidonHash(processId, rootHashBefore, batchNum)
// The result is reduced modulo the BLS12-381 scalar field order.
func ComputeEvaluationPoint(processID, rootHashBefore *big.Int, batchNum uint64) (z *big.Int, err error) {
	// Calculate z = PoseidonHash(processId, rootHashBefore, batchNum)
	z, err = poseidon.MultiPoseidon(processID, rootHashBefore, big.NewInt(int64(batchNum)))
	if err != nil {
		return nil, err
	}

	// Mask z to 250 bits
	z.And(z, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 250), big.NewInt(1)))

	return z, nil
}

// BuildKZGCommitment collects the raw batch-data, packs it into one blob and
// produces (blob, commitment, proof, z, y, versionedHash).
//
// blob layout:
//  1. ResultsAdd (types.FieldsPerBallot * 4 coordinates)
//  2. ResultsSub (types.FieldsPerBallot * 4 coordinates)
//  3. Votes sequentially until voteID = 0x0 (sentinel)
//     Each vote: voteID + address + encryptedBallot coordinates
func (st *State) BuildKZGCommitment(batchNum uint64) (
	blob *kzg4844.Blob,
	commit kzg4844.Commitment,
	proof kzg4844.Proof,
	z, y *big.Int,
	versionedHash [32]byte,
	err error,
) {
	blob = &kzg4844.Blob{}
	var cells [FieldElementsPerBlob][BytesPerFieldElement]byte
	cell := 0
	push := func(bi *big.Int) {
		if cell >= FieldElementsPerBlob {
			panic("blob overflow")
		}
		// Store blob cells in big-endian as expected by go-ethereum
		bi.FillBytes(cells[cell][:])
		cell++
	}

	// First, add results (always present)
	for _, p := range st.newResultsAdd.BigInts() {
		push(p)
	}
	for _, p := range st.newResultsSub.BigInts() {
		push(p)
	}

	// Then add votes sequentially (no padding)
	for _, v := range st.Votes() {
		push(new(big.Int).SetBytes(v.VoteID))  // voteId hash
		push(v.Address)                        // address
		for _, p := range v.Ballot.BigInts() { // ballot coordinates
			push(p)
		}
	}

	// Add sentinel (voteID = 0x0) to mark end of votes
	// remaining cells are zero-initialised already
	push(big.NewInt(0))

	// Convert 2D cell array to flat blob format
	// The blob is a fixed-size array (FieldElementsPerBlob * BytesPerFieldElement)
	for i := range FieldElementsPerBlob {
		start := i * BytesPerFieldElement
		end := start + BytesPerFieldElement
		copy(blob[start:end], cells[i][:])
	}

	// Generate KZG commitment
	commit, err = kzg4844.BlobToCommitment(blob)
	if err != nil {
		err = fmt.Errorf("blob_to_commitment failed: %w", err)
		return
	}

	// Find valid evaluation point z
	z, err = ComputeEvaluationPoint(st.processID, st.rootHashBefore, batchNum)
	if err != nil {
		return
	}

	// Generate KZG proof with the valid z
	var claim kzg4844.Claim
	proof, claim, err = kzg4844.ComputeProof(blob, BigIntToKZGPoint(z))
	if err != nil {
		err = fmt.Errorf("compute kzg4844 proof failed: %w", err)
		return
	}
	// Claim is already in big-endian format
	y = new(big.Int).SetBytes(claim[:])

	// Create versioned hash using SHA256 (as per kzg4844.CalcBlobHashV1)
	hasher := sha256.New()
	versionedHash = kzg4844.CalcBlobHashV1(hasher, &commit)

	return
}

// ParseBlobData extracts vote and results data from a blob
func ParseBlobData(blob *kzg4844.Blob) (*BlobData, error) {
	coordsPerBallot := types.FieldsPerBallot * 4 // each field has 4 coordinates (C1.X, C1.Y, C2.X, C2.Y)

	data := &BlobData{
		Votes:      make([]*Vote, 0),
		ResultsAdd: make([]*big.Int, coordsPerBallot),
		ResultsSub: make([]*big.Int, coordsPerBallot),
	}

	// extract big.Int from blob cell
	blobBytes := (*(*[131072]byte)(unsafe.Pointer(blob)))[:]
	getCell := func(cellIndex int) *big.Int {
		if cellIndex >= FieldElementsPerBlob {
			return big.NewInt(0)
		}
		start := cellIndex * BytesPerFieldElement
		cellBytes := blobBytes[start : start+BytesPerFieldElement]
		// Read blob cells as big-endian (canonical form)
		return new(big.Int).SetBytes(cellBytes)
	}

	cellIndex := 0

	// Extract ResultsAdd (first coordsPerBallot cells)
	for i := 0; i < coordsPerBallot; i++ {
		data.ResultsAdd[i] = getCell(cellIndex)
		cellIndex++
	}

	// Extract ResultsSub (next coordsPerBallot cells)
	for i := 0; i < coordsPerBallot; i++ {
		data.ResultsSub[i] = getCell(cellIndex)
		cellIndex++
	}

	// Extract votes until we find voteID = 0x0 (sentinel)
	for {
		voteID := getCell(cellIndex)
		cellIndex++

		// Check for sentinel (voteID = 0x0)
		if voteID.Cmp(big.NewInt(0)) == 0 {
			break
		}

		// Check if we have enough cells for a complete vote
		if cellIndex+1+coordsPerBallot > FieldElementsPerBlob {
			return nil, fmt.Errorf("incomplete vote data in blob")
		}

		// Extract address
		address := getCell(cellIndex)
		cellIndex++

		// Extract ballot coordinates
		ballotCoords := make([]*big.Int, coordsPerBallot)
		for j := 0; j < coordsPerBallot; j++ {
			ballotCoords[j] = getCell(cellIndex)
			cellIndex++
		}

		// Create ballot from coordinates using elgamal.NewBallot
		ballot, err := elgamal.NewBallot(Curve).SetBigInts(ballotCoords)
		if err != nil {
			return nil, err
		}

		// Convert voteID back to StateKeyMaxLen-byte array
		voteIDBytes := make([]byte, types.StateKeyMaxLen)
		voteID.FillBytes(voteIDBytes)

		vote := &Vote{
			Address: address,
			VoteID:  voteIDBytes,
			Ballot:  ballot,
		}
		data.Votes = append(data.Votes, vote)

		// Safety check to prevent infinite loop
		if len(data.Votes) > types.VotesPerBatch {
			return nil, fmt.Errorf("too many votes in blob")
		}
	}

	return data, nil
}

// CalculateMaxVotesPerBlob calculates the maximum number of votes that can fit in a blob
func CalculateMaxVotesPerBlob() int {
	coordsPerBallot := types.FieldsPerBallot * 4
	resultsCells := 2 * coordsPerBallot     // resultsAdd + resultsSub
	cellsPerVote := 1 + 1 + coordsPerBallot // voteID + address + ballot
	sentinelCells := 1                      // voteID = 0x0 sentinel

	availableCells := FieldElementsPerBlob - resultsCells - sentinelCells
	return availableCells / cellsPerVote
}

// ApplyBlobToState applies the data from a blob to restore state
func (st *State) ApplyBlobToState(blobData *BlobData) error {
	if err := st.StartBatch(); err != nil {
		return err
	}

	// Add votes from blob
	for _, vote := range blobData.Votes {
		if err := st.AddVote(vote); err != nil {
			return err
		}
	}

	// End the batch to calculate the results properly
	if err := st.EndBatch(); err != nil {
		return err
	}

	// Now set the final results from the blob data
	resultsAdd, err := elgamal.NewBallot(Curve).SetBigInts(blobData.ResultsAdd)
	if err != nil {
		return err
	}
	st.SetResultsAdd(resultsAdd)

	resultsSub, err := elgamal.NewBallot(Curve).SetBigInts(blobData.ResultsSub)
	if err != nil {
		return err
	}
	st.SetResultsSub(resultsSub)

	return nil
}

// BigIntToKZGPoint converts a big.Int to a kzg4844.Point.
func BigIntToKZGPoint(x *big.Int) kzg4844.Point {
	var point kzg4844.Point
	// Convert big.Int to big-endian byte array
	be := make([]byte, 32)
	x.FillBytes(be)
	copy(point[:], be[:])
	return point
}
