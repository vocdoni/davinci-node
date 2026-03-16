package prover

import (
	"os"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
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

// prover defaults to defaultProver but can be replaced using SetProver()
var prover ProverFunc = defaultProver

// ProverFunc defines a function type that matches the signature needed for zkSNARK proving.
// The function is generic enough to handle all circuit types.
// This type is used for dependency injection, particularly in the Sequencer.
type ProverFunc func(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	assignment frontend.Circuit,
	opts ...backend.ProverOption,
) (groth16.Proof, error)

// ProverWithWitnessFunc defines a function type for proving with an already-created witness.
// This is primarily used in test code where witnesses are already created.
type ProverWithWitnessFunc func(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	w witness.Witness,
	opts ...backend.ProverOption,
) (groth16.Proof, error)

// SetProver sets a custom prover function.
// This is particularly useful for tests that need to debug circuit execution.
func SetProver(p ProverFunc) {
	prover = p
}
