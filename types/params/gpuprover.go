package params

import (
	"os"

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
