package state

import (
	"fmt"
	"math/big"

	gethparams "github.com/ethereum/go-ethereum/params"

	"github.com/vocdoni/davinci-node/crypto/blobs"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

const (
	BlobTxBytesPerFieldElement = gethparams.BlobTxBytesPerFieldElement
	BlobTxFieldElementsPerBlob = gethparams.BlobTxFieldElementsPerBlob
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
//  1. ResultsAdd (params.FieldsPerBallot * 4 coordinates)
//  2. ResultsSub (params.FieldsPerBallot * 4 coordinates)
//  3. Votes sequentially until voteID = 0x0 (sentinel):
//     Each vote: voteID + address + reencryptedBallot coordinates
func (st *State) BuildKZGCommitment() (*blobs.BlobEvalData, error) {
	var cells [BlobTxFieldElementsPerBlob][BlobTxBytesPerFieldElement]byte
	cell := 0
	push := func(bi *big.Int) error {
		if cell >= BlobTxFieldElementsPerBlob {
			return fmt.Errorf("blob overflow")
		}
		biBytes := bi.Bytes()
		// Pad to 32 bytes if necessary (big-endian)
		if len(biBytes) < BlobTxBytesPerFieldElement {
			padded := make([]byte, BlobTxBytesPerFieldElement)
			copy(padded[BlobTxBytesPerFieldElement-len(biBytes):], biBytes)
			biBytes = padded
		}
		// Copy as big-endian
		copy(cells[cell][:], biBytes)
		cell++
		return nil
	}

	// First, add results (always present)
	for _, p := range st.newResultsAdd.BigInts() {
		if err := push(p); err != nil {
			return nil, err
		}
	}
	for _, p := range st.newResultsSub.BigInts() {
		if err := push(p); err != nil {
			return nil, err
		}
	}

	// Then add votes sequentially (no padding)
	for _, v := range st.Votes() {
		if err := push(v.VoteID.BigInt()); err != nil { // voteId hash
			return nil, err
		}
		if err := push(v.Address); err != nil { // address
			return nil, err
		}
		for _, p := range v.ReencryptedBallot.BigInts() { // reencrypted ballot coordinates
			if err := push(p); err != nil {
				return nil, err
			}
		}
	}

	// Add sentinel (voteID = 0x0) to mark end of votes
	// remaining cells are zero-initialised already
	if err := push(big.NewInt(0)); err != nil {
		return nil, err
	}

	// Convert 2D cell array to flat blob format
	// The blob is a fixed-size array (FieldElementsPerBlob * BytesPerFieldElement)
	blob := new(types.Blob)
	for i := range BlobTxFieldElementsPerBlob {
		start := i * BlobTxBytesPerFieldElement
		end := start + BlobTxBytesPerFieldElement
		copy(blob[start:end], cells[i][:])
	}

	// Compute KZG commitment first
	commitment, err := blob.ComputeCommitment()
	if err != nil {
		return nil, fmt.Errorf("failed to compute commitment: %w", err)
	}

	// Compute evaluation point z from commitment and blob
	z, err := blobs.ComputeEvaluationPoint(st.processID.MathBigInt(), st.rootHashBefore, commitment)
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

// ParseBlobData extracts vote and results data from a blob.
func ParseBlobData(blob []byte) (*BlobData, error) {
	if len(blob) != types.BlobLength {
		return nil, fmt.Errorf("unexpected blob length %d", len(blob))
	}

	coordsPerBallot := params.FieldsPerBallot * 4 // each field has 4 coordinates (C1.X, C1.Y, C2.X, C2.Y)

	data := &BlobData{
		Votes:      make([]*Vote, 0),
		ResultsAdd: make([]*big.Int, coordsPerBallot),
		ResultsSub: make([]*big.Int, coordsPerBallot),
	}

	// extract big.Int from blob cell
	getCell := func(cellIndex int) *big.Int {
		if cellIndex >= BlobTxFieldElementsPerBlob {
			return big.NewInt(0)
		}
		start := cellIndex * BlobTxBytesPerFieldElement
		cellBytes := blob[start : start+BlobTxBytesPerFieldElement]
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
		voteIDcell := getCell(cellIndex)
		cellIndex++

		// Check for sentinel (voteID = 0x0)
		if voteIDcell.Cmp(big.NewInt(0)) == 0 {
			break
		}

		// Check if we have enough cells for a complete vote
		if cellIndex+1+coordsPerBallot > BlobTxFieldElementsPerBlob {
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

		// Convert voteID back to types.VoteID
		voteID, err := types.BigIntToVoteID(voteIDcell)
		if err != nil {
			return nil, err
		}

		vote := &Vote{
			Address:           address,
			VoteID:            voteID,
			ReencryptedBallot: ballot,
		}
		data.Votes = append(data.Votes, vote)

		// Safety check to prevent infinite loop
		if len(data.Votes) > params.VotesPerBatch {
			return nil, fmt.Errorf("too many votes in blob")
		}
	}

	return data, nil
}

// ApplyBlobToState applies the data from a blob to restore state
func (st *State) ApplyBlobToState(blob *types.Blob) error {
	blobData, err := ParseBlobData(blob.Bytes())
	if err != nil {
		return err
	}

	// Add votes directly to the state tree without batch processing
	for _, vote := range blobData.Votes {
		// Add or update the vote ballot in the tree
		ballotIndex := types.CalculateBallotIndex(vote.Address, types.IndexTODO)
		if _, err := st.EncryptedBallot(ballotIndex); err != nil {
			// Key doesn't exist, add it
			if err := st.tree.AddBigInt(ballotIndex.BigInt(), vote.ReencryptedBallot.BigInts()...); err != nil {
				return fmt.Errorf("failed to add vote with address %d to tree: %w", vote.Address, err)
			}
		} else {
			// Key exists, update it
			if err := st.tree.UpdateBigInt(ballotIndex.BigInt(), vote.ReencryptedBallot.BigInts()...); err != nil {
				return fmt.Errorf("failed to update vote with address %d in tree: %w", vote.Address, err)
			}
		}

		// Add or update the vote ID in the tree
		if !st.ContainsVoteID(vote.VoteID) {
			// Key doesn't exist, add it
			if err := st.tree.AddBigInt(vote.VoteID.BigInt(), VoteIDKeyValue); err != nil {
				return fmt.Errorf("failed to add vote ID %d to tree: %w", vote.VoteID, err)
			}
		} else {
			// Key exists, update it
			if err := st.tree.UpdateBigInt(vote.VoteID.BigInt(), VoteIDKeyValue); err != nil {
				return fmt.Errorf("failed to update vote ID %d in tree: %w", vote.VoteID, err)
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
