package results

import (
	"fmt"
	"time"

	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/spec/params"
)

// Compile compiles the ResultsVerifier circuit definition.
func Compile() (constraint.ConstraintSystem, error) {
	startTime := time.Now()
	log.Infow("compiling circuit definition", "circuit", Artifacts.Name())
	ccs, err := frontend.Compile(params.ResultsVerifierCurve.ScalarField(), r1cs.NewBuilder, &ResultsVerifierCircuit{})
	if err != nil {
		return nil, fmt.Errorf("compile results verifier circuit: %w", err)
	}
	log.DebugTime("circuit definition compiled", startTime, "circuit", Artifacts.Name())
	return ccs, nil
}
