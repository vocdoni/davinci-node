//go:build icicle

package prover

import (
	"fmt"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/accelerated/icicle"
	gpugroth16 "github.com/consensys/gnark/backend/accelerated/icicle/groth16"
	icicle_bls12377 "github.com/consensys/gnark/backend/accelerated/icicle/groth16/bls12-377"
	icicle_bls12381 "github.com/consensys/gnark/backend/accelerated/icicle/groth16/bls12-381"
	icicle_bn254 "github.com/consensys/gnark/backend/accelerated/icicle/groth16/bn254"
	icicle_bw6761 "github.com/consensys/gnark/backend/accelerated/icicle/groth16/bw6-761"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/vocdoni/davinci-node/types"
)

// callGPUProver is a helper function that calls the GPU prover with proper curve-specific type assertions.
// The icicle GPU library requires concrete curve-specific types, not generic interfaces.
func callGPUProver(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	w witness.Witness,
	icicleOpts []icicle.Option,
) (groth16.Proof, error) {
	// Type assert the proving key to the concrete curve-specific type
	switch curve {
	case ecc.BN254:
		bn254Pk, ok := pk.(*icicle_bn254.ProvingKey)
		if !ok {
			return nil, fmt.Errorf("proving key type mismatch for BN254: expected *icicle_bn254.ProvingKey, got %T", pk)
		}
		return gpugroth16.Prove(ccs, bn254Pk, w, icicleOpts...)

	case ecc.BLS12_377:
		bls12377Pk, ok := pk.(*icicle_bls12377.ProvingKey)
		if !ok {
			return nil, fmt.Errorf("proving key type mismatch for BLS12_377: expected *icicle_bls12_377.ProvingKey, got %T", pk)
		}
		return gpugroth16.Prove(ccs, bls12377Pk, w, icicleOpts...)

	case ecc.BLS12_381:
		bls12381Pk, ok := pk.(*icicle_bls12381.ProvingKey)
		if !ok {
			return nil, fmt.Errorf("proving key type mismatch for BLS12_381: expected *icicle_bls12_381.ProvingKey, got %T", pk)
		}
		return gpugroth16.Prove(ccs, bls12381Pk, w, icicleOpts...)

	case ecc.BW6_761:
		bw6761Pk, ok := pk.(*icicle_bw6761.ProvingKey)
		if !ok {
			return nil, fmt.Errorf("proving key type mismatch for BW6_761: expected *icicle_bw6_761.ProvingKey, got %T", pk)
		}
		return gpugroth16.Prove(ccs, bw6761Pk, w, icicleOpts...)

	default:
		return nil, fmt.Errorf("GPU proving not supported for curve %s", curve)
	}
}

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
	if types.UseGPUProver {
		proof, err := GPUProver(curve, ccs, pk, assignment, opts...)
		if err != nil {
			// GPU proving failed, fall back to CPU
			fmt.Printf("GPU proving failed (%v), falling back to CPU\n", err)
			return CPUProver(curve, ccs, pk, assignment, opts...)
		}
		return proof, nil
	}
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
	return baseCPUProver(curve, ccs, cpuReadyProvingKey(pk), assignment, opts...)
}

// GPUProver is an implementation that uses GPU acceleration for proving.
func GPUProver(
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
	return gpuProve(curve, ccs, pk, w, opts...)
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
	if types.UseGPUProver {
		proof, err := GPUProverWithWitness(curve, ccs, pk, w, opts...)
		if err != nil {
			// GPU proving failed, fall back to CPU
			fmt.Printf("GPU proving failed (%v), falling back to CPU\n", err)
			return CPUProverWithWitness(curve, ccs, pk, w, opts...)
		}
		return proof, nil
	}
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
	return baseCPUProverWithWitness(ccs, cpuReadyProvingKey(pk), w, opts...)
}

// GPUProverWithWitness proves using GPU with an already-created witness.
func GPUProverWithWitness(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	w witness.Witness,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	return gpuProve(curve, ccs, pk, w, opts...)
}

// gpuProve performs GPU-based proving with icicle acceleration.
func gpuProve(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	w witness.Witness,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	// Convert backend.ProverOption to icicle.Option
	var icicleOpts []icicle.Option
	if len(opts) > 0 {
		icicleOpts = append(icicleOpts, icicle.WithProverOptions(opts...))
	}
	return callGPUProver(curve, ccs, pk, w, icicleOpts)
}

// cpuReadyProvingKey extracts the standard proving key from icicle wrapper types.
func cpuReadyProvingKey(pk groth16.ProvingKey) groth16.ProvingKey {
	switch t := pk.(type) {
	case *icicle_bn254.ProvingKey:
		return &t.ProvingKey
	case *icicle_bls12377.ProvingKey:
		return &t.ProvingKey
	case *icicle_bls12381.ProvingKey:
		return &t.ProvingKey
	case *icicle_bw6761.ProvingKey:
		return &t.ProvingKey
	default:
		return pk
	}
}
