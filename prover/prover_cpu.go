//go:build !icicle

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

// DefaultProver is the default prover implementation.
// It uses the GPU prover if UseGPUProver is true, otherwise it uses the CPU prover.
// If GPU proving fails, it falls back to CPU proving.
func DefaultProver(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	assignment frontend.Circuit,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	return CPUProver(curve, ccs, pk, assignment, opts...)
}

// CPUProver is the standard implementation that simply calls groth16.Prove directly.
// This is used in production environments.
func CPUProver(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	assignment frontend.Circuit,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	w, err := frontend.NewWitness(assignment, curve.ScalarField())
	if err != nil {
		return nil, fmt.Errorf("failed to create witness: %w", err)
	}
	return groth16.Prove(ccs, pk, w, opts...)
}

// GPUProver is an implementation that uses GPU acceleration for proving.
func GPUProver(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	assignment frontend.Circuit,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	panic("GPU prover not supported in this build")
}

// ProveWithWitness generates a proof from an already-created witness.
// It automatically uses GPU acceleration if UseGPUProver is true.
// If GPU proving fails, it falls back to CPU proving.
func ProveWithWitness(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	w witness.Witness,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	return CPUProverWithWitness(curve, ccs, pk, w, opts...)
}

// CPUProverWithWitness proves using CPU with an already-created witness.
func CPUProverWithWitness(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	w witness.Witness,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	return groth16.Prove(ccs, pk, w, opts...)
}

// GPUProverWithWitness proves using GPU with an already-created witness.
func GPUProverWithWitness(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	w witness.Witness,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	panic("GPU prover not supported in this build")
}
