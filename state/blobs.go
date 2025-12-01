package state

import (
	"fmt"
	"math/big"
	"unsafe"

	gethkzg "github.com/ethereum/go-ethereum/crypto/kzg4844"

	"github.com/vocdoni/davinci-node/crypto/blobs"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/types"
)

// BlobData represents the structured data extracted from a blob
type BlobData struct {
	Votes      []*Vote
	ResultsAdd []*big.Int
	ResultsSub []*big.Int
}

// BuildKZGCommitment collects the raw batch-data, packs it into one blob and
// produces (blob, commitment, proof, z, y, versionedHash).
//
// blob layout:
//  1. ResultsAdd (types.FieldsPerBallot * 4 coordinates)
//  2. ResultsSub (types.FieldsPerBallot * 4 coordinates)
//  3. Votes sequentially until voteID = 0x0 (sentinel):
//     Each vote: voteID + address + reencryptedBallot coordinates
func (st *State) BuildKZGCommitment() (*blobs.BlobEvalData, error) {
	var cells [blobs.FieldElementsPerBlob][blobs.BytesPerFieldElement]byte
	cell := 0
	push := func(bi *big.Int) {
		if cell >= blobs.FieldElementsPerBlob {
			panic("blob overflow")
		}
		biBytes := bi.Bytes()
		// Pad to 32 bytes if necessary (big-endian)
		if len(biBytes) < blobs.BytesPerFieldElement {
			padded := make([]byte, blobs.BytesPerFieldElement)
			copy(padded[blobs.BytesPerFieldElement-len(biBytes):], biBytes)
			biBytes = padded
		}
		// Copy as big-endian
		copy(cells[cell][:], biBytes)
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
		push(new(big.Int).SetBytes(v.VoteID))             // voteId hash
		push(v.Address)                                   // address
		for _, p := range v.ReencryptedBallot.BigInts() { // reencrypted ballot coordinates
			push(p)
		}
	}

	// Add sentinel (voteID = 0x0) to mark end of votes
	// remaining cells are zero-initialised already
	push(big.NewInt(0))

	// Convert 2D cell array to flat blob format
	// The blob is a fixed-size array (FieldElementsPerBlob * BytesPerFieldElement)
	blob := new(gethkzg.Blob)
	for i := range blobs.FieldElementsPerBlob {
		start := i * blobs.BytesPerFieldElement
		end := start + blobs.BytesPerFieldElement
		copy(blob[start:end], cells[i][:])
	}

	// Compute KZG commitment first
	commitment, err := gethkzg.BlobToCommitment(blob)
	if err != nil {
		return nil, fmt.Errorf("failed to compute commitment: %w", err)
	}

	// Compute evaluation point z from commitment
	z, err := blobs.ComputeEvaluationPoint(st.processID, st.rootHashBefore, commitment)
	if err != nil {
		return nil, err
	}

	blobData, err := new(blobs.BlobEvalData).Set(blob, z)
	if err != nil {
		err = fmt.Errorf("set blob eval data failed: %w", err)
		return nil, err
	}

	return blobData, err
}

// ParseBlobData extracts vote and results data from a blob
func ParseBlobData(blob *gethkzg.Blob) (*BlobData, error) {
	coordsPerBallot := types.FieldsPerBallot * 4 // each field has 4 coordinates (C1.X, C1.Y, C2.X, C2.Y)

	data := &BlobData{
		Votes:      make([]*Vote, 0),
		ResultsAdd: make([]*big.Int, coordsPerBallot),
		ResultsSub: make([]*big.Int, coordsPerBallot),
	}

	// extract big.Int from blob cell
	blobBytes := (*(*[131072]byte)(unsafe.Pointer(blob)))[:]
	getCell := func(cellIndex int) *big.Int {
		if cellIndex >= blobs.FieldElementsPerBlob {
			return big.NewInt(0)
		}
		start := cellIndex * blobs.BytesPerFieldElement
		cellBytes := blobBytes[start : start+blobs.BytesPerFieldElement]
		// Read blob cells as big-endian (canonical form)
		return new(big.Int).SetBytes(cellBytes)
	}

	cellIndex := 0

	// Extract ResultsAdd (first coordsPerBallot cells)
	for i := range coordsPerBallot {
		data.ResultsAdd[i] = getCell(cellIndex)
		cellIndex++
	}

	// Extract ResultsSub (next coordsPerBallot cells)
	for i := range coordsPerBallot {
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
		if cellIndex+1+coordsPerBallot > blobs.FieldElementsPerBlob {
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

// ApplyBlobToState applies the data from a blob to restore state
func (st *State) ApplyBlobToState(blobData *BlobData) error {
	// Add votes directly to the state tree without batch processing
	for _, vote := range blobData.Votes {
		// For votes parsed from blob, we need to set ReencryptedBallot to the same as Ballot
		// since we don't store the reencrypted version separately in the blob
		vote.ReencryptedBallot = vote.Ballot

		// Add or update the vote ballot in the tree
		_, _, err := st.tree.GetBigInt(vote.Address)
		if err != nil {
			// Key doesn't exist, add it
			if err := st.tree.AddBigInt(vote.Address, vote.ReencryptedBallot.BigInts()...); err != nil {
				return fmt.Errorf("failed to add vote to tree: %w", err)
			}
		} else {
			// Key exists, update it
			if err := st.tree.UpdateBigInt(vote.Address, vote.ReencryptedBallot.BigInts()...); err != nil {
				return fmt.Errorf("failed to update vote in tree: %w", err)
			}
		}

		// Add or update the vote ID in the tree
		voteIDKey := vote.VoteID.BigInt().MathBigInt()
		_, _, err = st.tree.GetBigInt(voteIDKey)
		if err != nil {
			// Key doesn't exist, add it
			if err := st.tree.AddBigInt(voteIDKey, VoteIDKeyValue); err != nil {
				return fmt.Errorf("failed to add vote ID to tree: %w", err)
			}
		} else {
			// Key exists, update it
			if err := st.tree.UpdateBigInt(voteIDKey, VoteIDKeyValue); err != nil {
				return fmt.Errorf("failed to update vote ID in tree: %w", err)
			}
		}
	}

	// Set the results from the blob data directly
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
