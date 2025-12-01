package blobs

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"strings"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bls12381"
	"github.com/consensys/gnark/std/math/emulated"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethkzg "github.com/ethereum/go-ethereum/crypto/kzg4844"
	gethparams "github.com/ethereum/go-ethereum/params"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
	"github.com/vocdoni/davinci-node/crypto/hash/poseidon"
	gnarkposeidon "github.com/vocdoni/gnark-crypto-primitives/hash/bn254/poseidon"
)

const (
	// Number of field elements stored in a single data blob
	FieldElementsPerBlob = gethparams.BlobTxFieldElementsPerBlob
	// Size in bytes of a field element
	BytesPerFieldElement = gethparams.BlobTxBytesPerFieldElement
)

// BlobEvalData holds the evaluation data for a blob.
// It is useful for preparing data for zk-SNARK proving and Ethereum transactions.
type BlobEvalData struct {
	ForGnark struct {
		Z            frontend.Variable // value within bn254 field
		Y            emulated.Element[FE]
		Blob         [N]frontend.Variable // values within bn254 field
		OpeningProof sw_bls12381.G1Affine
		Commitment   sw_bls12381.G1Affine
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

	// Convert KZG inputs to gnark format
	b.ForGnark.Commitment, b.ForGnark.OpeningProof, b.Y, err = KZGToCircuitInputs(b.Commitment, b.OpeningProof, claim)
	if err != nil {
		return nil, fmt.Errorf("failed to convert KZG inputs: %w", err)
	}

	// Set evaluation point (y)
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

// CommitmentToLimbs splits a 48-byte KZG commitment into 3 × 16-byte limbs for Poseidon hashing.
func CommitmentToLimbs(commitment gethkzg.Commitment) [3]*big.Int {
	return [3]*big.Int{
		new(big.Int).SetBytes(commitment[0:16]),
		new(big.Int).SetBytes(commitment[16:32]),
		new(big.Int).SetBytes(commitment[32:48]),
	}
}

// ComputeEvaluationPoint computes evaluation point z using Poseidon hash.
// z = Poseidon(processID | rootHashBefore | C)
// where C is the KZG commitment split into 3 × 16-byte limbs.
func ComputeEvaluationPoint(processID, rootHashBefore *big.Int, commitment gethkzg.Commitment) (*big.Int, error) {
	// Split 48-byte commitment into 3 × 16-byte limbs
	limbs := CommitmentToLimbs(commitment)

	z, err := poseidon.MultiPoseidon(processID, rootHashBefore, limbs[0], limbs[1], limbs[2])
	if err != nil {
		return nil, err
	}

	return z, nil
}

// ComputeEvaluationPointInCircuit computes the evaluation point z in-circuit using Poseidon hash.
// This is the Gnark circuit version of ComputeEvaluationPoint.
// z = Poseidon(processID | rootHashBefore | C)
// where C is the KZG commitment represented as 3 × 16-byte limbs.
func ComputeEvaluationPointInCircuit(
	api frontend.API,
	processID frontend.Variable,
	rootHashBefore frontend.Variable,
	commitmentLimbs [3]frontend.Variable,
) (frontend.Variable, error) {
	z, err := gnarkposeidon.MultiHash(api, processID, rootHashBefore, commitmentLimbs[0], commitmentLimbs[1], commitmentLimbs[2])
	if err != nil {
		return nil, fmt.Errorf("poseidon hash failed: %w", err)
	}
	return z, nil
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
