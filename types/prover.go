package types

import (
	"os"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
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
