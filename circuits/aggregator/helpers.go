package aggregator

import (
	"fmt"

	groth16bls12377 "github.com/consensys/gnark/backend/groth16/bls12-377"

	bls12377 "github.com/consensys/gnark-crypto/ecc/bls12-377"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	"github.com/consensys/gnark/std/math/emulated"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
)

// FillWithDummy fills the aggregator assignment with dummy proofs
// and witnesses compiled for the main constraint.ConstraintSystem provided and
// the proving key. It generates dummy proofs using the inner verification key
// provided. It starts to fill from the index provided. Returns an error if
// something fails.
func (assignment *AggregatorCircuit) FillWithDummy(fromIdx int) error {
	_, _, g1Gen, g2Gen := bls12377.Generators()
	dummyProof := &groth16bls12377.Proof{
		Ar:  g1Gen,
		Bs:  g2Gen,
		Krs: g1Gen,
		Commitments: []bls12377.G1Affine{
			g1Gen,
		},
		CommitmentPok: g1Gen,
	}

	// prepare dummy proof to recursion
	recursiveDummyProof, err := stdgroth16.ValueOfProof[sw_bls12377.G1Affine, sw_bls12377.G2Affine](dummyProof)
	if err != nil {
		return fmt.Errorf("dummy proof value error: %w", err)
	}
	// Fill the assignment with dummy values from the first unused slot onward.
	for i := fromIdx; i < len(assignment.Proofs); i++ {
		assignment.BallotHashes[i] = emulated.Element[sw_bn254.ScalarField]{
			Limbs: []frontend.Variable{1, 0, 0, 0},
		}
		assignment.Proofs[i] = recursiveDummyProof
	}
	return nil
}
