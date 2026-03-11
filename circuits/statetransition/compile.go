package statetransition

import (
	"fmt"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/spec/params"
)

// Compile compiles the StateTransition circuit definition from the inner
// aggregator CCS and verifying key.
func Compile(aggregatorCCS constraint.ConstraintSystem, aggregatorVK groth16.VerifyingKey) (constraint.ConstraintSystem, error) {
	startTime := time.Now()
	log.Infow("compiling circuit definition", "circuit", Artifacts.Name())
	aggregatorFixedVK, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl](aggregatorVK)
	if err != nil {
		return nil, fmt.Errorf("fix aggregator verification key: %w", err)
	}
	placeholder := &StateTransitionCircuit{
		AggregatorProof: stdgroth16.PlaceholderProof[sw_bw6761.G1Affine, sw_bw6761.G2Affine](aggregatorCCS),
		AggregatorVK:    aggregatorFixedVK,
	}
	ccs, err := frontend.Compile(params.StateTransitionCurve.ScalarField(), r1cs.NewBuilder, placeholder)
	if err != nil {
		return nil, fmt.Errorf("compile statetransition circuit: %w", err)
	}
	log.DebugTime("circuit definition compiled", startTime, "circuit", Artifacts.Name())
	return ccs, nil
}
