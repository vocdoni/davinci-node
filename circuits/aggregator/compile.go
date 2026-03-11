package aggregator

import (
	"fmt"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/spec/params"
)

// Compile compiles the Aggregator circuit definition from the inner vote
// verifier CCS and verifying key.
func Compile(voteVerifierCCS constraint.ConstraintSystem, voteVerifierVK groth16.VerifyingKey) (constraint.ConstraintSystem, error) {
	startTime := time.Now()
	log.Infow("compiling circuit definition", "circuit", Artifacts.Name())
	voteVerifierFixedVK, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bls12377.G1Affine, sw_bls12377.G2Affine, sw_bls12377.GT](voteVerifierVK)
	if err != nil {
		return nil, fmt.Errorf("fix vote verifier verification key: %w", err)
	}
	placeholder := &AggregatorCircuit{
		Proofs:          [params.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{},
		VerificationKey: voteVerifierFixedVK,
	}
	for i := range params.VotesPerBatch {
		placeholder.Proofs[i] = stdgroth16.PlaceholderProof[sw_bls12377.G1Affine, sw_bls12377.G2Affine](voteVerifierCCS)
	}
	ccs, err := frontend.Compile(params.AggregatorCurve.ScalarField(), r1cs.NewBuilder, placeholder)
	if err != nil {
		return nil, fmt.Errorf("compile aggregator circuit: %w", err)
	}
	log.DebugTime("circuit definition compiled", startTime, "circuit", Artifacts.Name())
	return ccs, nil
}
