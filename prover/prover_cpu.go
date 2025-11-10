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
// Without icicle, this always uses CPU proving.
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
func CPUProver(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	assignment frontend.Circuit,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	return baseCPUProver(curve, ccs, pk, assignment, opts...)
}

// GPUProver is not available without icicle build tag.
func GPUProver(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	assignment frontend.Circuit,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	return nil, fmt.Errorf("GPU proving not available: build with -tags=icicle")
}

// ProveWithWitness generates a proof from an already-created witness.
// Without icicle, this always uses CPU proving.
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
	return baseCPUProverWithWitness(ccs, pk, w, opts...)
}

// GPUProverWithWitness is not available without icicle build tag.
func GPUProverWithWitness(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	w witness.Witness,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	return nil, fmt.Errorf("GPU proving not available: build with -tags=icicle")
}
