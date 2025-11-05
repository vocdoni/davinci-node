package prover

import (
	"fmt"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/accelerated/icicle"
	gpugroth16 "github.com/consensys/gnark/backend/accelerated/icicle/groth16"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/consensys/gnark/test"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	ballottest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	teststatetransition "github.com/vocdoni/davinci-node/circuits/test/statetransition"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util/circomgnark"
)

func init() {
	// Set the default prover in the types package to avoid circular dependencies
	// This allows circuit packages to use the prover without importing this package
	types.DefaultProver = defaultProverImpl
}

// defaultProverImpl is the internal implementation that gets assigned to types.DefaultProver
func defaultProverImpl(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	assignment frontend.Circuit,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	if types.UseGPUProver {
		return GPUProver(curve, ccs, pk, assignment, opts...)
	}
	return CPUProver(curve, ccs, pk, assignment, opts...)
}

// DefaultProver is a convenience wrapper that calls the default prover implementation.
// It uses the GPU prover if UseGPUProver is true, otherwise it uses the CPU prover.
// This function can be used directly or accessed via types.DefaultProver.
func DefaultProver(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	assignment frontend.Circuit,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	return defaultProverImpl(curve, ccs, pk, assignment, opts...)
}

// CPUProver is the standard implementation that simply calls groth16.Prove directly.
// This is used in production environments.
func CPUProver(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	assignment frontend.Circuit,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	// Create a witness from the circuit
	witness, err := frontend.NewWitness(assignment, curve.ScalarField())
	if err != nil {
		return nil, fmt.Errorf("failed to create witness: %w", err)
	}

	// Generate the proof
	return groth16.Prove(ccs, pk, witness, opts...)
}

// GPUProver is an implementation that uses GPU acceleration for proving.
func GPUProver(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	assignment frontend.Circuit,
	opts ...backend.ProverOption,
) (groth16.Proof, error) {
	// Create a witness from the circuit
	witness, err := frontend.NewWitness(assignment, curve.ScalarField())
	if err != nil {
		return nil, fmt.Errorf("failed to create witness: %w", err)
	}
	log.Debugw("using GPU prover", "curve", curve.String())

	// Convert backend.ProverOption to icicle.Option
	var icicleOpts []icicle.Option
	if len(opts) > 0 {
		icicleOpts = append(icicleOpts, icicle.WithProverOptions(opts...))
	}
	// Generate the proof using GPU acceleration with converted options
	return gpugroth16.Prove(ccs, pk, witness, icicleOpts...)
}

// NewDebugProver creates a prover that runs test.IsSolved before normal proving.
// This is used in test environments to debug circuit execution.
//
// Parameters:
//   - t: The testing.T instance from the test
//
// Returns a ProverFunc that will execute test.IsSolved and then groth16.Prove
func NewDebugProver(t *testing.T) types.ProverFunc {
	return func(
		curve ecc.ID,
		ccs constraint.ConstraintSystem,
		pk groth16.ProvingKey,
		assignment frontend.Circuit,
		opts ...backend.ProverOption,
	) (groth16.Proof, error) {
		var placeholder frontend.Circuit

		switch assignment.(type) {
		case *voteverifier.VerifyVoteCircuit:
			t.Logf("running debug prover for voteverifier")
			circomPlaceholder, err := circomgnark.Circom2GnarkPlaceholder(
				ballottest.TestCircomVerificationKey, circuits.BallotProofNPubInputs)
			if err != nil {
				t.Fatal(err)
			}
			placeholder = &voteverifier.VerifyVoteCircuit{
				CircomProof:           circomPlaceholder.Proof,
				CircomVerificationKey: circomPlaceholder.Vk,
			}
		case *aggregator.AggregatorCircuit:
			t.Logf("running debug prover for aggregator")
			vvk, err := voteverifier.Artifacts.VerifyingKey()
			if err != nil {
				t.Fatal(err)
			}
			fixedVk, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bls12377.G1Affine, sw_bls12377.G2Affine, sw_bls12377.GT](vvk)
			if err != nil {
				t.Fatal(err)
			}
			p := &aggregator.AggregatorCircuit{
				Proofs:          [types.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{},
				VerificationKey: fixedVk,
			}
			ccs, err := aggregator.Artifacts.CircuitDefinition()
			if err != nil {
				t.Fatal(err)
			}
			for i := range types.VotesPerBatch {
				p.Proofs[i] = stdgroth16.PlaceholderProof[sw_bls12377.G1Affine, sw_bls12377.G2Affine](ccs)
			}
			placeholder = p
		case *statetransition.StateTransitionCircuit:
			t.Logf("running debug prover for statetransition")
			agVk, err := aggregator.Artifacts.VerifyingKey()
			if err != nil {
				t.Fatal(err)
			}
			p := teststatetransition.CircuitPlaceholder()
			fixedVk, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl](agVk)
			if err != nil {
				t.Fatal(err)
			}
			p.AggregatorVK = fixedVk
			placeholder = p
		default:
			t.Fatalf("unsupported circuit type: %T", assignment)

		}

		// First run the circuit solver verification for debugging
		assert := test.NewAssert(t)
		startTime := time.Now()
		assert.SolvingSucceeded(placeholder, assignment,
			test.WithCurves(curve),
			test.WithBackends(backend.GROTH16),
			test.WithProverOpts(opts...),
		)
		t.Logf("debug prover succeeded for %T, took %s", assignment, time.Since(startTime).String())

		// Then do the normal proof generation
		witness, err := frontend.NewWitness(assignment, curve.ScalarField())
		if err != nil {
			return nil, fmt.Errorf("failed to create witness: %w", err)
		}

		// Generate the proof
		t.Logf("running groth16.Prove for %T", assignment)
		return groth16.Prove(ccs, pk, witness, opts...)
	}
}
