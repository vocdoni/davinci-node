package results

import (
	"fmt"

	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/vocdoni/davinci-node/spec/params"
)

// Compile compiles the ResultsVerifier circuit definition.
func Compile() (constraint.ConstraintSystem, error) {
	ccs, err := frontend.Compile(params.ResultsVerifierCurve.ScalarField(), r1cs.NewBuilder, &ResultsVerifierCircuit{})
	if err != nil {
		return nil, fmt.Errorf("compile results verifier circuit: %w", err)
	}
	return ccs, nil
}
