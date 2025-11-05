package types

import (
	"os"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/accelerated/icicle"
	gpugroth16 "github.com/consensys/gnark/backend/accelerated/icicle/groth16"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/vocdoni/davinci-node/log"
)

// UseGPUProver indicates whether to use the GPU-accelerated prover, using Icicle.
var UseGPUProver = false

func init() {
	if os.Getenv("GPU_PROVER") == "true" ||
		os.Getenv("GPU_PROVER") == "y" ||
		os.Getenv("GPU_PROVER") == "1" ||
		os.Getenv("GPU_PROVER") == "yes" {
		UseGPUProver = true
	}
	log.Infow("GPU prover usage", "enabled", UseGPUProver)
}

// ProverFunc defines a function type that matches the signature needed for zkSNARK proving
// in the Sequencer package. The function is generic enough to handle all circuit types.
type ProverFunc func(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	assignment frontend.Circuit,
	opts ...backend.ProverOption,
) (groth16.Proof, error)

// DefaultProver is a package-level variable that holds the default prover implementation.
// It is set by the prover package during initialization and can be CPU or GPU based
// on the UseGPUProver flag.
var DefaultProver ProverFunc

// ProveWithWitness generates a proof from a witness, automatically using GPU
// acceleration if enabled via UseGPUProver flag.
// This is primarily used in test code where witnesses are already created.
func ProveWithWitness(
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	w witness.Witness,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	if UseGPUProver {
		// Convert backend.ProverOption to icicle.Option for GPU prover
		var icicleOpts []icicle.Option
		if len(opts) > 0 {
			icicleOpts = append(icicleOpts, icicle.WithProverOptions(opts...))
		}
		return gpugroth16.Prove(ccs, pk, w, icicleOpts...)
	}
	return groth16.Prove(ccs, pk, w, opts...)
}
