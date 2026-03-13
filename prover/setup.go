package prover

import (
	"time"

	gpugroth16 "github.com/consensys/gnark/backend/accelerated/icicle/groth16"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// Setup wraps groth16.Setup and switches to the ICICLE-aware setup when the GPU
// prover is enabled. This guarantees that the resulting proving key has the
// extra device metadata required by gpugroth16.Prove.
func Setup(ccs constraint.ConstraintSystem) (pk groth16.ProvingKey, vk groth16.VerifyingKey, err error) {
	log.Debugw("generating circuit keys", "gpu", types.UseGPUProver, "constraints", ccs.GetNbConstraints())
	startTime := time.Now()
	defer func() {
		if err == nil {
			log.DebugTime("circuit keys setup done", startTime, "gpu", types.UseGPUProver, "constraints", ccs.GetNbConstraints())
		}
	}()

	if types.UseGPUProver {
		return gpugroth16.Setup(ccs)
	}
	return groth16.Setup(ccs)
}
