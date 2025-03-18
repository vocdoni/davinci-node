package aggregator

import (
	"fmt"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	"github.com/consensys/gnark/std/math/emulated"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/voteverifier"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_iden3"
)

// FillWithDummy function fills the assignments provided with a dummy proofs
// and witnesses compiled for the main constraint.ConstraintSystem provided and
// the proving key. It generates dummy proofs using the inner verification key
// provided. It starts to fill from the index provided. Returns an error if
// something fails.
func (assignments *AggregatorCircuit) FillWithDummy(mainCCS constraint.ConstraintSystem,
	mainPk groth16.ProvingKey, innerVk []byte, fromIdx int,
) error {
	// get dummy proof witness
	dummyWitness, err := voteverifier.DummyWitness(innerVk, new(bjj.BJJ).New())
	if err != nil {
		return fmt.Errorf("dummy witness error: %w", err)
	}
	// generate dummy proof
	dummyProof, err := groth16.Prove(mainCCS, mainPk, dummyWitness, stdgroth16.GetNativeProverOptions(circuits.AggregatorCurve.ScalarField(), circuits.VoteVerifierCurve.ScalarField()))
	if err != nil {
		return fmt.Errorf("proof error: %w", err)
	}
	// prepare dummy proof to recursion
	recursiveDummyProof, err := stdgroth16.ValueOfProof[sw_bls12377.G1Affine, sw_bls12377.G2Affine](dummyProof)
	if err != nil {
		return fmt.Errorf("dummy proof value error: %w", err)
	}
	// fill placeholders and assignments dummy values
	for i := fromIdx; i < len(assignments.Proofs); i++ {
		assignments.ProofsInputsHashes[i] = emulated.Element[sw_bn254.ScalarField]{
			Limbs: []frontend.Variable{1, 0, 0, 0},
		}
		assignments.Proofs[i] = recursiveDummyProof
	}
	return nil
}
