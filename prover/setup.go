package prover

import (
	"github.com/consensys/gnark-crypto/ecc"
	gpugroth16 "github.com/consensys/gnark/backend/accelerated/icicle/groth16"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/vocdoni/davinci-node/types"
)

// Setup wraps groth16.Setup and switches to the ICICLE-aware setup when the GPU
// prover is enabled. This guarantees that the resulting proving key has the
// extra device metadata required by gpugroth16.Prove.
func Setup(ccs constraint.ConstraintSystem) (groth16.ProvingKey, groth16.VerifyingKey, error) {
	if types.UseGPUProver {
		return gpugroth16.Setup(ccs)
	}
	return groth16.Setup(ccs)
}

// NewProvingKey instantiates an empty proving key compatible with the selected
// backend. When GPU proving is enabled this returns an ICICLE proving key so
// that serialized keys can be read directly into GPU-ready structures.
func NewProvingKey(curve ecc.ID) groth16.ProvingKey {
	if types.UseGPUProver {
		return gpugroth16.NewProvingKey(curve)
	}
	return groth16.NewProvingKey(curve)
}
