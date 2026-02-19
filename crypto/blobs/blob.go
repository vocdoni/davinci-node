package blobs

import (
	"fmt"
	"math/big"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
	"github.com/vocdoni/davinci-node/crypto/hash/poseidon"
	"github.com/vocdoni/davinci-node/types"
	gnarkposeidon "github.com/vocdoni/gnark-crypto-primitives/hash/native/bn254/poseidon"
)

// BlobEvalData holds the evaluation data for a blob.
// It is useful for preparing data for zk-SNARK proving and Ethereum transactions.
type BlobEvalData struct {
	ForGnark struct {
		CommitmentLimbs [3]frontend.Variable
		ProofLimbs      [3]frontend.Variable
		Y               emulated.Element[FE]
		Blob            [N]frontend.Variable // values within bn254 field
	}
	Commitment      types.KZGCommitment
	CommitmentLimbs [3]*big.Int
	Z               *big.Int
	Y               *big.Int
	Ylimbs          [4]*big.Int
	Blob            *types.Blob
	OpeningProof    types.KZGProof
	ProofLimbs      [3]*big.Int
	// Cell proofs for EIP-7594 (Fusaka upgrade)
	// Each blob has CellProofsPerBlob (128) cell proofs
	CellProofs [types.CellProofsPerBlob]types.KZGProof
}

// Set initializes the BlobEvalData with the given blob, claim, and evaluation point z.
// Computes the KZG cell proofs (EIP-7594) and sets the relevant fields.
// It returns itself for chaining.
func (b *BlobEvalData) Set(blob *types.Blob, z *big.Int) (*BlobEvalData, error) {
	commitment, cellProofs, err := blob.ComputeCommitmentAndCellProofs()
	if err != nil {
		return nil, fmt.Errorf("failed to compute: %w", err)
	}
	b.Commitment = commitment
	copy(b.CellProofs[:], cellProofs)

	// Compute the point evaluation proof to get the claim (y value) and OpeningProof
	proof, claim, err := blob.ComputeProof(z)
	if err != nil {
		return nil, err
	}
	b.OpeningProof = proof
	b.Y = claim

	// Extract commitment limbs (3 × 16 bytes)
	b.CommitmentLimbs = b.Commitment.ToLimbs()
	b.ForGnark.CommitmentLimbs = b.Commitment.ToGnarkLimbs()

	// Extract proof limbs (3 × 16 bytes)
	b.ProofLimbs = b.OpeningProof.ToLimbs()
	b.ForGnark.ProofLimbs = b.OpeningProof.ToGnarkLimbs()

	// Set evaluation point (y) for Gnark
	b.ForGnark.Y = emulated.ValueOf[FE](b.Y)

	// Extract Y limbs as big.Int
	// Note that we cannot access b.ForGnark.Y.Limbs because the decomposition is performed async while witness processing
	Ylimbs, err := format.SplitYForBn254FromBLS12381(b.Y)
	if err != nil {
		return nil, fmt.Errorf("failed to split Y into limbs: %w", err)
	}

	// Assign limbs to the array
	b.Ylimbs = Ylimbs

	// Set evaluation point (z)
	b.Z = new(big.Int).Set(z)

	// Convert blob to gnark circuit format
	b.ForGnark.Blob = blob.ToGnark()

	// Deep copy for safety
	b.Blob = new(types.Blob)
	*b.Blob = *blob

	return b, err
}

// TxSidecar returns a BlobTxSidecar with a single KZG blob, commitment, and 128 cell proofs.
// Returns a Version 1 sidecar with cell proofs for EIP-7594 (Fusaka upgrade).
func (b *BlobEvalData) TxSidecar() *types.BlobTxSidecar {
	return types.NewBlobTxSidecar(
		types.BlobTxSidecarVersion1,
		[]*types.Blob{b.Blob},
		[]types.KZGCommitment{b.Commitment},
		b.CellProofs[:],
	)
}

// ComputeEvaluationPoint computes evaluation point z using Poseidon hash.
// z = Poseidon(processID | rootHashBefore | C | blob)
// where C is the KZG commitment split into 3 × 16-byte limbs.
func ComputeEvaluationPoint(processID, rootHashBefore *big.Int, commitment types.KZGCommitment) (*big.Int, error) {
	// Split 48-byte commitment into 3 × 16-byte limbs
	limbs := commitment.ToLimbs()

	// Prepare inputs: processID, rootHashBefore, 3 commitment limbs
	inputs := make([]*big.Int, 5)
	inputs[0] = processID
	inputs[1] = rootHashBefore
	inputs[2] = limbs[0]
	inputs[3] = limbs[1]
	inputs[4] = limbs[2]

	z, err := poseidon.MultiPoseidon(inputs...)
	if err != nil {
		return nil, err
	}

	return z, nil
}

// ComputeEvaluationPointInCircuit computes the evaluation point z in-circuit using Poseidon hash.
// This is the Gnark circuit version of ComputeEvaluationPoint.
// z = Poseidon(processID | rootHashBefore | C | blob)
// where C is the KZG commitment represented as 3 × 16-byte limbs.
func ComputeEvaluationPointInCircuit(
	api frontend.API,
	processID frontend.Variable,
	rootHashBefore frontend.Variable,
	commitmentLimbs [3]frontend.Variable,
) (frontend.Variable, error) {
	// Pre-allocate slice
	inputs := make([]frontend.Variable, 5)
	inputs[0] = processID
	inputs[1] = rootHashBefore
	inputs[2] = commitmentLimbs[0]
	inputs[3] = commitmentLimbs[1]
	inputs[4] = commitmentLimbs[2]

	z, err := gnarkposeidon.MultiHash(api, inputs...)
	if err != nil {
		return nil, fmt.Errorf("poseidon hash failed: %w", err)
	}
	return z, nil
}
