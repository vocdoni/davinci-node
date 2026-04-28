package state

import (
	"errors"
	"fmt"
	"math/big"

	gethparams "github.com/ethereum/go-ethereum/params"
	"github.com/vocdoni/arbo"

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
	Results     []*big.Int
	VotersCount uint64
	Votes       []*Vote
}

func (b *Batch) computeBlobEvalData() (*blobs.BlobEvalData, error) {
	var cells [BlobTxFieldElementsPerBlob][BlobTxBytesPerFieldElement]byte
	cell := 0
	push := func(name string, bi *big.Int) error {
		if cell >= BlobTxFieldElementsPerBlob {
			return fmt.Errorf("blob overflow")
		}
		if bi == nil {
			return fmt.Errorf("%s is nil", name)
		}
		if bi.Sign() < 0 {
			return fmt.Errorf("%s is negative", name)
		}
		if bi.BitLen() > BlobTxBytesPerFieldElement*8 {
			return fmt.Errorf("%s exceeds %d bytes", name, BlobTxBytesPerFieldElement)
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
	for _, p := range b.newResults.BigInts() {
		if err := push("results coordinate", p); err != nil {
			return nil, err
		}
	}
	if err := push("voters count", big.NewInt(int64(b.VotersCount()))); err != nil {
		return nil, err
	}

	// Then add exactly VotersCount votes sequentially (no padding)
	for _, v := range b.Votes() {
		if err := push("vote ID", v.VoteID.BigInt()); err != nil {
			return nil, err
		}
		if err := push("vote address", v.Address); err != nil {
			return nil, err
		}
		if err := push("vote ballot index", v.BallotIndex.BigInt()); err != nil {
			return nil, err
		}
		if err := push("vote weight", v.Weight); err != nil {
			return nil, err
		}
		for _, p := range v.ReencryptedBallot.BigInts() {
			if err := push("reencrypted ballot coordinate", p); err != nil {
				return nil, err
			}
		}
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
	z, err := blobs.ComputeEvaluationPoint(b.state.processID.MathBigInt(), b.rootHashBefore, commitment)
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
		VotersCount: 0,
		Votes:       make([]*Vote, 0),
		Results:     make([]*big.Int, coordsPerBallot),
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

	// Extract Results (first coordsPerBallot cells)
	for i := range coordsPerBallot {
		data.Results[i] = getCell(cellIndex)
		cellIndex++
	}
	votersCountCell := getCell(cellIndex)
	cellIndex++
	if !votersCountCell.IsUint64() {
		return nil, fmt.Errorf("invalid voters count in blob")
	}
	votersCount := votersCountCell.Uint64()
	if votersCount > params.VotesPerBatch {
		return nil, fmt.Errorf("too many votes in blob")
	}
	data.VotersCount = votersCount

	// Extract exactly VotersCount votes.
	for range data.VotersCount {
		// Check if we have enough cells for a complete vote:
		// voteID + address + ballotIndex + weight + ballot coordinates.
		if cellIndex+4+coordsPerBallot > BlobTxFieldElementsPerBlob {
			return nil, fmt.Errorf("incomplete vote data in blob")
		}
		voteIDcell := getCell(cellIndex)
		cellIndex++

		// Extract address
		address := getCell(cellIndex)
		cellIndex++

		// Extract ballotIndex
		ballotIndexCell := getCell(cellIndex)
		cellIndex++

		weight := getCell(cellIndex)
		cellIndex++

		// Extract ballot coordinates
		ballotCoords := make([]*big.Int, coordsPerBallot)
		for j := range coordsPerBallot {
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

		// Convert ballotIndex back to types.VoteID
		ballotIndex, err := types.BigIntToBallotIndex(ballotIndexCell)
		if err != nil {
			return nil, err
		}

		vote := &Vote{
			Address:           address,
			BallotIndex:       ballotIndex,
			VoteID:            voteID,
			Weight:            weight,
			ReencryptedBallot: ballot,
		}
		data.Votes = append(data.Votes, vote)
	}

	return data, nil
}

// ApplyBlobToState applies the data from a blob to restore state
func (s *State) ApplyBlobToState(blob *types.Blob) (err error) {
	if blob == nil {
		return fmt.Errorf("nil blob")
	}
	blobData, err := ParseBlobData(blob.Bytes())
	if err != nil {
		return err
	}

	treeTx := s.newTreeTx()
	defer func() {
		if err != nil {
			treeTx.discard()
		}
	}()

	if err := treeTx.applyBlobData(blobData); err != nil {
		return err
	}
	return treeTx.commit("apply blob to state")
}

// ApplyBlobSidecarFromRoot atomically applies a blob sidecar from rootBefore and
// commits only if the staged root matches rootAfter.
func (s *State) ApplyBlobSidecarFromRoot(rootBefore, rootAfter *big.Int, sidecar *types.BlobTxSidecar) (err error) {
	if rootBefore == nil {
		return fmt.Errorf("nil root before")
	}
	if rootAfter == nil {
		return fmt.Errorf("nil root after")
	}
	if sidecar == nil {
		return fmt.Errorf("nil blob sidecar")
	}

	treeTx := s.newTreeTx()
	defer func() {
		if err != nil {
			treeTx.discard()
		}
	}()

	if err := treeTx.SetRootAsBigInt(rootBefore); err != nil {
		return fmt.Errorf("set root before blob sidecar: %w", err)
	}
	if err := treeTx.applyBlobSidecar(sidecar); err != nil {
		return fmt.Errorf("apply blob sidecar from root: %w", err)
	}

	stagedRoot, err := treeTx.RootAsBigInt()
	if err != nil {
		return fmt.Errorf("get restored state root: %w", err)
	}
	if stagedRoot.Cmp(rootAfter) != 0 {
		return fmt.Errorf("restored state root mismatch: expected %s, got %s",
			rootAfter.String(), stagedRoot.String())
	}

	return treeTx.commit("apply blob sidecar from root")
}

func (tx *stateTreeTx) applyBlobData(blobData *BlobData) error {
	if tx.tx == nil {
		return fmt.Errorf("need active state transaction")
	}

	// Add votes directly to the state tree without batch processing
	for _, vote := range blobData.Votes {
		// Add or update the vote ballot in the tree
		if _, err := tx.EncryptedBallot(vote.BallotIndex); err != nil {
			if !errors.Is(err, ErrKeyNotFound) {
				return fmt.Errorf("failed to get vote with ballot index %s from tree: %w",
					vote.BallotIndex.String(), err)
			}
			// Key doesn't exist, add it
			if err := tx.addBigInt(vote.BallotIndex.BigInt(), vote.TreeLeafValues()...); err != nil {
				return fmt.Errorf("failed to add vote with address %s to tree: %w", vote.Address.String(), err)
			}
		} else {
			// Key exists, update it
			if err := tx.updateBigInt(vote.BallotIndex.BigInt(), vote.TreeLeafValues()...); err != nil {
				return fmt.Errorf("failed to update vote with address %s in tree: %w", vote.Address.String(), err)
			}
		}

		// Add the vote ID in the tree
		if _, _, err := tx.getBigInt(vote.VoteID.BigInt()); err == nil {
			return fmt.Errorf("failed to add vote ID %d to tree: already exists", vote.VoteID)
		} else if !errors.Is(err, arbo.ErrKeyNotFound) {
			return fmt.Errorf("failed to check vote ID %d in tree: %w", vote.VoteID, err)
		}
		if err := tx.addBigInt(vote.VoteID.BigInt(), voteIDLeafValue); err != nil {
			return fmt.Errorf("failed to add vote ID %d to tree: %w", vote.VoteID, err)
		}
	}

	// Set the results from the blob data directly
	results, err := elgamal.NewBallot(Curve).SetBigInts(blobData.Results)
	if err != nil {
		return err
	}
	if err := tx.setResults(results); err != nil {
		return fmt.Errorf("failed to set results from blob: %w", err)
	}

	return nil
}

// ApplyBlobSidecar applies every blob in the provided sidecar to the state.
func (s *State) ApplyBlobSidecar(sidecar *types.BlobTxSidecar) (err error) {
	if sidecar == nil {
		return fmt.Errorf("nil blob sidecar")
	}

	treeTx := s.newTreeTx()
	defer func() {
		if err != nil {
			treeTx.discard()
		}
	}()

	if err := treeTx.applyBlobSidecar(sidecar); err != nil {
		return err
	}

	return treeTx.commit("apply blob sidecar")
}

func (tx *stateTreeTx) applyBlobSidecar(sidecar *types.BlobTxSidecar) error {
	if sidecar == nil {
		return fmt.Errorf("nil blob sidecar")
	}
	for _, blob := range sidecar.Blobs {
		if blob == nil {
			continue
		}
		blobData, err := ParseBlobData(blob.Bytes())
		if err != nil {
			return fmt.Errorf("failed to parse blob sidecar: %w", err)
		}
		if err := tx.applyBlobData(blobData); err != nil {
			return fmt.Errorf("failed to apply blob sidecar: %w", err)
		}
	}
	return nil
}
