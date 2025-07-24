package blobs

import (
	"fmt"
	"math/big"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
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

// BLS12-381 scalar-field modulus (used for KZG)
var pBLS = bls12381.ID.ScalarField()

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

func ComputeEvaluationPoint2(processID, rootHashBefore *big.Int, batchNum uint64) (z *big.Int, err error) {
	// Calculate z = PoseidonHash(processId, rootHashBefore, batchNum)
	z, err = poseidon.MultiPoseidon(processID, rootHashBefore, big.NewInt(int64(batchNum)))
	if err != nil {
		return nil, err
	}

	// Mask z to 250 bits
	z.And(z, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 250), big.NewInt(1)))

	return z, nil
}

// ComputeEvaluationPoint computes evaluation point z using Poseidon hash.
// z = PoseidonHash(processId, rootHashBefore, batchNum, blob_hash)
// We hash the entire blob first to avoid exceeding MultiPoseidon's 256 input limit
// This function ensures z never equals any of the 4096 omega roots of unity to avoid division by zero
func ComputeEvaluationPoint(processID, rootHashBefore *big.Int, batchNum uint64, blob *kzg4844.Blob) (z *big.Int, err error) {
	// First, compute a hash of the entire blob by processing it in chunks
	// MultiPoseidon has a limit of 256 inputs, but we have 4096 blob elements
	var blobHash *big.Int
	chunkSize := 200 // Use 200 to stay well under the 256 limit
	chunkHashes := make([]*big.Int, 0)

	for start := 0; start < FieldElementsPerBlob; start += chunkSize {
		end := start + chunkSize
		if end > FieldElementsPerBlob {
			end = FieldElementsPerBlob
		}

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
		// Calculate z = PoseidonHash(processId, rootHashBefore, batchNum, blobHash, nonce)
		z, err = poseidon.MultiPoseidon(processID, rootHashBefore, big.NewInt(int64(batchNum)), blobHash, nonce)
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

// CreateBlobFromCoefficients creates a blob in evaluation form from polynomial coefficients.
// The input coefficients represent a polynomial p(x) = coeffs[0] + coeffs[1]*x + coeffs[2]*x^2 + ...
// The output blob contains p(omega[i]) for each domain root omega[i] where i = 0..4095.
//
// This is the correct way to construct blobs for KZG, as blobs represent polynomials
// in evaluation form over the multiplicative subgroup of order 4096.
func CreateBlobFromCoefficients(coeffs []*big.Int) (*kzg4844.Blob, error) {
	if len(coeffs) > FieldElementsPerBlob {
		return nil, fmt.Errorf("too many coefficients: got %d, max %d", len(coeffs), FieldElementsPerBlob)
	}

	// Generate the domain roots (omega values) in natural order
	mod := pBLS // BLS12-381 scalar field modulus
	pMinus1 := new(big.Int).Sub(mod, big.NewInt(1))
	generator := big.NewInt(5)
	exponent := new(big.Int).Div(pMinus1, big.NewInt(FieldElementsPerBlob))
	primitiveRoot := new(big.Int).Exp(generator, exponent, mod)

	// Generate all domain roots: omega[i] = primitiveRoot^i
	domainRoots := make([]*big.Int, FieldElementsPerBlob)
	domainRoots[0] = big.NewInt(1)
	for i := 1; i < FieldElementsPerBlob; i++ {
		domainRoots[i] = new(big.Int).Mul(domainRoots[i-1], primitiveRoot)
		domainRoots[i].Mod(domainRoots[i], mod)
	}

	// Create blob in evaluation form: blob[i] = p(domainRoots[i])
	blob := &kzg4844.Blob{}
	for i := 0; i < FieldElementsPerBlob; i++ {
		// Evaluate polynomial at domainRoots[i]
		// p(x) = coeffs[0] + coeffs[1]*x + coeffs[2]*x^2 + ...
		evaluation := big.NewInt(0)
		if len(coeffs) > 0 {
			evaluation.Set(coeffs[0]) // coeffs[0] (constant term)
		}

		// Add higher-order terms: coeffs[j] * domainRoots[i]^j
		xPower := big.NewInt(1) // domainRoots[i]^0 = 1
		for j := 1; j < len(coeffs); j++ {
			xPower.Mul(xPower, domainRoots[i]).Mod(xPower, mod) // domainRoots[i]^j
			term := new(big.Int).Mul(coeffs[j], xPower)
			term.Mod(term, mod)
			evaluation.Add(evaluation, term).Mod(evaluation, mod)
		}

		// Store evaluation in blob (big-endian format)
		evaluation.FillBytes(blob[i*BytesPerFieldElement : (i+1)*BytesPerFieldElement])
	}

	return blob, nil
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
