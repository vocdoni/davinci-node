package blobs

import (
	"math/big"

	bn254 "github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/vocdoni/davinci-node/crypto/hash/poseidon"
	"github.com/vocdoni/davinci-node/types"
)

// FieldElementsPerBlob defines the number of field elements per blob
const FieldElementsPerBlob = 4096

// BytesPerFieldElement defines the number of bytes per field element
const BytesPerFieldElement = 32

// BN254 scalar-field modulus
var pBN = bn254.ID.ScalarField()

// ComputeProof computes the KZG proof for a given blob and evaluation point z.
func ComputeProof(blob *kzg4844.Blob, z *big.Int) (kzg4844.Proof, kzg4844.Claim, error) {
	// Convert z to a kzg4844.Point
	kzgPoint := BigIntToKZGPoint(z)

	// Compute the KZG proof
	proof, claim, err := kzg4844.ComputeProof(blob, kzgPoint)
	if err != nil {
		return kzg4844.Proof{}, kzg4844.Claim{}, err
	}

	return proof, claim, nil
}

// BlobToCommitment converts a blob to its KZG commitment.
func BlobToCommitment(blob *kzg4844.Blob) (kzg4844.Commitment, error) {
	// Compute the commitment from the blob
	commitment, err := kzg4844.BlobToCommitment(blob)
	if err != nil {
		return kzg4844.Commitment{}, err
	}

	return commitment, nil
}

// ComputeEvaluationPoint computes evaluation point z using Poseidon hash.
// z = PoseidonHash(processId, rootHashBefore, batchNum, blob_elements...)
// Including blob data in the hash improves soundness by binding the challenge to the actual data
func ComputeEvaluationPoint(processID, rootHashBefore *big.Int, batchNum uint64, blob *kzg4844.Blob) (z *big.Int, err error) {
	// Start with the original inputs
	inputs := []*big.Int{processID, rootHashBefore, big.NewInt(int64(batchNum))}

	// Add blob elements to the hash inputs
	// We include all non-zero blob elements for soundness
	for i := range FieldElementsPerBlob {
		cellBytes := blob[i*BytesPerFieldElement : (i+1)*BytesPerFieldElement]
		cellValue := new(big.Int).SetBytes(cellBytes)

		// Only include non-zero values to save computation
		if cellValue.Sign() != 0 {
			inputs = append(inputs, cellValue)
		}
	}

	// Calculate z = PoseidonHash(processId, rootHashBefore, batchNum, blob_elements...)
	z, err = poseidon.MultiPoseidon(inputs...)
	if err != nil {
		return nil, err
	}

	// Mask z to 250 bits
	z.And(z, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 250), big.NewInt(1)))

	return z, nil
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

// BigIntToKZGPoint converts a big.Int to a kzg4844.Point.
func BigIntToKZGPoint(x *big.Int) kzg4844.Point {
	var point kzg4844.Point
	// Convert big.Int to big-endian byte array
	be := make([]byte, 32)
	x.FillBytes(be)
	copy(point[:], be[:])
	return point
}
