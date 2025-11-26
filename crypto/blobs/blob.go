package blobs

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"strings"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	bn254 "github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/math/emulated"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethkzg "github.com/ethereum/go-ethereum/crypto/kzg4844"
	gethparams "github.com/ethereum/go-ethereum/params"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
	"github.com/vocdoni/davinci-node/crypto/hash/poseidon"
	"github.com/vocdoni/davinci-node/types"
)

const (
	// Number of field elements stored in a single data blob
	FieldElementsPerBlob = gethparams.BlobTxFieldElementsPerBlob
	// Size in bytes of a field element
	BytesPerFieldElement = gethparams.BlobTxBytesPerFieldElement
)

// BN254 scalar-field modulus
var pBN = bn254.ID.ScalarField()

// BLS12-381 scalar-field modulus (used for KZG)
var pBLS = bls12381.ID.ScalarField()

// BlobEvalData holds the evaluation data for a blob.
// It is useful for preparing data for zk-SNARK proving and Ethereum transactions.
type BlobEvalData struct {
	ForGnark struct {
		Z    frontend.Variable // value within bn254 field
		Y    emulated.Element[FE]
		Blob [N]frontend.Variable // values within bn254 field
	}
	Commitment gethkzg.Commitment
	Z          *big.Int
	Y          *big.Int
	Ylimbs     [4]*big.Int
	Blob       gethkzg.Blob
	// Opening proof for point-evaluation precompile (integrity check)
	OpeningProof gethkzg.Proof
	// Cell proofs for EIP-7594 (Fusaka upgrade)
	// Each blob has CellProofsPerBlob (128) cell proofs
	CellProofs [gethkzg.CellProofsPerBlob]gethkzg.Proof
}

// Set initializes the BlobEvalData with the given blob, claim, and evaluation point z.
// Computes the KZG cell proofs (EIP-7594) and sets the relevant fields.
// It returns itself for chaining.
func (b *BlobEvalData) Set(blob *gethkzg.Blob, z *big.Int) (*BlobEvalData, error) {
	commitment, cellProofs, err := ComputeCommitmentAndCellProofs(blob)
	if err != nil {
		return nil, fmt.Errorf("failed to compute: %w", err)
	}
	b.Commitment = commitment
	copy(b.CellProofs[:], cellProofs)

	// Compute the point evaluation proof to get the claim (y value) and OpeningProof
	openingProof, claim, err := gethkzg.ComputeProof(blob, BigIntToPoint(z))
	if err != nil {
		return nil, err
	}
	b.OpeningProof = openingProof

	// Set evaluation point (y)
	b.Y = new(big.Int).SetBytes(claim[:])
	b.ForGnark.Y = emulated.ValueOf[FE](b.Y)
	// Extract limbs as big.Int
	// Note that we cannot access b.ForGnark.Y.Limbs because the decomposicion is performed async while witness processing
	Ylimbs, err := format.SplitYForBn254FromBLS12381(b.Y)
	if err != nil {
		return nil, fmt.Errorf("failed to split Y into limbs: %w", err)
	}

	// Assign limbs to the array
	b.Ylimbs = [4]*big.Int{
		Ylimbs[0],
		Ylimbs[1],
		Ylimbs[2],
		Ylimbs[3],
	}

	// Set evaluation point (z)
	b.ForGnark.Z = z
	b.Z = new(big.Int).Set(z)

	// Convert blob to gnark circuit format
	for i := range FieldElementsPerBlob {
		b.ForGnark.Blob[i] = new(big.Int).SetBytes(
			blob[i*BytesPerFieldElement : (i+1)*BytesPerFieldElement],
		)
	}

	// Copy for safety
	copy(b.Blob[:], blob[:])

	return b, err
}

// TxSidecar returns a BlobTxSidecar with a single KZG blob, commitment, and 128 cell proofs.
// Returns a Version 1 sidecar with cell proofs for EIP-7594 (Fusaka upgrade).
func (b *BlobEvalData) TxSidecar() *gethtypes.BlobTxSidecar {
	return gethtypes.NewBlobTxSidecar(
		gethtypes.BlobSidecarVersion1,
		[]gethkzg.Blob{b.Blob},
		[]gethkzg.Commitment{b.Commitment},
		b.CellProofs[:],
	)
}

// String returns a string representation of the blob data.
func (b *BlobEvalData) String() string {
	str := strings.Builder{}
	for i := range b.Blob {
		str.WriteString(fmt.Sprintf("[%x]", b.Blob[i*BytesPerFieldElement:(i+1)*BytesPerFieldElement]))
		if i == FieldElementsPerBlob-1 {
			break
		}
	}
	return str.String()
}

// HashV1 calculates the 'versioned blob hash' of a commitment.
func (b *BlobEvalData) HashV1() (vh [32]byte) {
	return gethkzg.CalcBlobHashV1(sha256.New(), &b.Commitment)
}

// ComputeEvaluationPoint computes evaluation point z using Poseidon hash.
// z = PoseidonHash(processId, rootHashBefore, batchNum, blob_hash)
// We hash the entire blob first to avoid exceeding MultiPoseidon's 256 input limit
// This function ensures z never equals any of the 4096 omega roots of unity to avoid division by zero
func ComputeEvaluationPoint(processID, rootHashBefore *big.Int, blob *gethkzg.Blob) (z *big.Int, err error) {
	// First, compute a hash of the entire blob by processing it in chunks
	// MultiPoseidon has a limit of 256 inputs, but we have 4096 blob elements
	var blobHash *big.Int
	chunkSize := 200 // Use 200 to stay well under the 256 limit
	chunkHashes := make([]*big.Int, 0)

	for start := 0; start < FieldElementsPerBlob; start += chunkSize {
		end := min(start+chunkSize, FieldElementsPerBlob)

		// Extract this chunk of blob elements
		chunk := make([]*big.Int, 0, end-start)
		for i := start; i < end; i++ {
			cellBytes := blob[i*BytesPerFieldElement : (i+1)*BytesPerFieldElement]
			cellValue := new(big.Int).SetBytes(cellBytes)
			// Reduce modulo BN254 field for Poseidon compatibility
			cellValue.Mod(cellValue, pBN)
			chunk = append(chunk, cellValue)
		}

		// Hash this chunk
		chunkHash, err := poseidon.MultiPoseidon(chunk...)
		if err != nil {
			return nil, err
		}
		chunkHashes = append(chunkHashes, chunkHash)
	}

	// Hash all the chunk hashes together to get the final blob hash
	blobHash, err = poseidon.MultiPoseidon(chunkHashes...)
	if err != nil {
		return nil, err
	}

	// Generate omega values to check against (using BLS12-381 scalar field)
	mod := pBLS // BLS12-381 scalar field modulus
	rMinus1 := new(big.Int).Sub(mod, big.NewInt(1))
	generator := big.NewInt(5)
	exponent := new(big.Int).Div(rMinus1, big.NewInt(4096))
	omega := new(big.Int).Exp(generator, exponent, mod)

	// Generate all 4096 omega values for collision checking
	omegas := make([]*big.Int, 4096)
	omegas[0] = big.NewInt(1)
	for i := 1; i < 4096; i++ {
		omegas[i] = new(big.Int).Mul(omegas[i-1], omega)
		omegas[i].Mod(omegas[i], mod)
	}

	// Try different nonces until we get a z that doesn't equal any omega
	nonce := big.NewInt(0)
	for {
		// Calculate z = PoseidonHash(processId, rootHashBefore, blobHash, nonce)
		z, err = poseidon.MultiPoseidon(processID, rootHashBefore, blobHash, nonce)
		if err != nil {
			return nil, err
		}

		// Mask z to 250 bits
		z.And(z, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 250), big.NewInt(1)))

		// Check if z equals any omega value
		collision := false
		for _, omegaVal := range omegas {
			if z.Cmp(omegaVal) == 0 {
				collision = true
				break
			}
		}

		// If no collision, we're done
		if !collision {
			break
		}

		// Otherwise, increment nonce and try again
		nonce.Add(nonce, big.NewInt(1))
	}

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

// ComputeCommitmentAndProof calculates the commitment and blob proof of the passed blob, using geth kzg4844.
// This can be used to construct a Version 0 BlobTxSidecar.
func ComputeCommitmentAndProof(blob *gethkzg.Blob) (gethkzg.Commitment, gethkzg.Proof, error) {
	commitment, err := gethkzg.BlobToCommitment(blob)
	if err != nil {
		return gethkzg.Commitment{}, gethkzg.Proof{}, fmt.Errorf("commitment: %w", err)
	}
	proof, err := gethkzg.ComputeBlobProof(blob, commitment)
	if err != nil {
		return gethkzg.Commitment{}, gethkzg.Proof{}, fmt.Errorf("blob proof: %w", err)
	}
	return commitment, proof, nil
}

// ComputeCommitmentAndCellProofs calculates the commitment and cell proofs of the passed blob, using geth kzg4844.
// This can be used to construct a Version 1 BlobTxSidecar.
func ComputeCommitmentAndCellProofs(blob *gethkzg.Blob) (gethkzg.Commitment, []gethkzg.Proof, error) {
	commitment, err := gethkzg.BlobToCommitment(blob)
	if err != nil {
		return gethkzg.Commitment{}, nil, fmt.Errorf("commitment: %w", err)
	}
	proofs, err := gethkzg.ComputeCellProofs(blob)
	if err != nil {
		return gethkzg.Commitment{}, nil, fmt.Errorf("cell proofs: %w", err)
	}
	return commitment, proofs, nil
}

// BigIntToPoint converts a big.Int to a gethkzg.Point (big-endian byte array)
func BigIntToPoint(x *big.Int) gethkzg.Point {
	var point gethkzg.Point
	x.FillBytes(point[:])
	return point
}
