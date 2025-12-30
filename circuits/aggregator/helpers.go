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
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_iden3"
	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
)

// FillWithDummy function fills the assignments provided with a dummy proofs
// and witnesses compiled for the main constraint.ConstraintSystem provided and
// the proving key. It generates dummy proofs using the inner verification key
// provided. It starts to fill from the index provided. Returns an error if
// something fails.
// If proverFn is nil, it uses the DefaultProver implementation.
func (assignments *AggregatorCircuit) FillWithDummy(mainCCS constraint.ConstraintSystem,
	mainPk groth16.ProvingKey, innerVk []byte, fromIdx int, proverFn types.ProverFunc,
) error {
	// get dummy proof witness
	assignment, err := voteverifier.DummyAssignment(innerVk, new(bjj.BJJ).New())
	if err != nil {
		return fmt.Errorf("dummy assignment error: %w", err)
	}

	var dummyProof groth16.Proof

	// If a custom prover function is provided, use it (e.g., for debug proving)
	// Otherwise, use prover.DefaultProver which supports GPU if enabled
	if proverFn != nil {
		// Use custom prover function (for debug/testing)
		dummyProof, err = proverFn(
			params.VoteVerifierCurve,
			mainCCS,
			mainPk,
			assignment,
			stdgroth16.GetNativeProverOptions(params.AggregatorCurve.ScalarField(),
				params.VoteVerifierCurve.ScalarField()),
		)
	} else {
		// Use the default prover (supports GPU if enabled)
		dummyProof, err = prover.DefaultProver(
			params.VoteVerifierCurve,
			mainCCS,
			mainPk,
			assignment,
			stdgroth16.GetNativeProverOptions(params.AggregatorCurve.ScalarField(),
				params.VoteVerifierCurve.ScalarField()),
		)
	}
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
