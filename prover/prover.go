package prover

import (
	"fmt"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
)

// createWitness is a helper function to create a witness from a circuit assignment.
func createWitness(assignment frontend.Circuit, curve ecc.ID) (witness.Witness, error) {
	w, err := frontend.NewWitness(assignment, curve.ScalarField())
	if err != nil {
		return nil, fmt.Errorf("failed to create witness: %w", err)
	}
	return w, nil
}

// cpuProve performs CPU-based proving with a standard proving key.
func cpuProve(
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	w witness.Witness,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	return groth16.Prove(ccs, pk, w, opts...)
}

// baseCPUProver implements CPU proving with witness creation.
// This is used by both CPU-only and GPU builds.
func baseCPUProver(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	assignment frontend.Circuit,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	w, err := createWitness(assignment, curve)
	if err != nil {
		return nil, err
	}
	return cpuProve(ccs, pk, w, opts...)
}

// baseCPUProverWithWitness implements CPU proving with an existing witness.
// This is used by both CPU-only and GPU builds.
func baseCPUProverWithWitness(
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	w witness.Witness,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	return cpuProve(ccs, pk, w, opts...)
}
