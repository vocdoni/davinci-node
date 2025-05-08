package sequencer

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/consensys/gnark/test"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/aggregator"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/statetransition"
	ballottest "github.com/vocdoni/vocdoni-z-sandbox/circuits/test/ballotproof"
	teststatetransition "github.com/vocdoni/vocdoni-z-sandbox/circuits/test/statetransition"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/voteverifier"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// ProverFunc defines a function type that matches the signature needed for zkSNARK proving
// in the Sequencer package. The function is generic enough to handle all circuit types.
type ProverFunc func(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	assignment frontend.Circuit,
	opts ...backend.ProverOption,
) (groth16.Proof, error)

// DefaultProver is the standard implementation that simply calls groth16.Prove directly.
// This is used in production environments.
func DefaultProver(
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

// NewDebugProver creates a prover that runs test.IsSolved before normal proving.
// This is used in test environments to debug circuit execution.
//
// Parameters:
//   - t: The testing.T instance from the test
//
// Returns a ProverFunc that will execute test.IsSolved and then groth16.Prove
func NewDebugProver(t *testing.T) ProverFunc {
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
			circomPlaceholder, err := circuits.Circom2GnarkPlaceholder(ballottest.TestCircomVerificationKey)
			if err != nil {
				t.Fatal(err)
			}
			placeholder = &voteverifier.VerifyVoteCircuit{
				CircomProof:           circomPlaceholder.Proof,
				CircomVerificationKey: circomPlaceholder.Vk,
			}
		case *aggregator.AggregatorCircuit:
			// fix the vote verifier verification key
			vvk := groth16.NewVerifyingKey(curve)
			if _, err := vvk.UnsafeReadFrom(bytes.NewReader(voteverifier.Artifacts.VerifyingKey())); err != nil {
				t.Fatal(err)
			}
			fixedVk, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bls12377.G1Affine, sw_bls12377.G2Affine, sw_bls12377.GT](vvk)
			if err != nil {
				t.Fatal(err)
			}
			// create final placeholder
			placeholder = &aggregator.AggregatorCircuit{
				Proofs:          [types.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{},
				VerificationKey: fixedVk,
			}

		case *statetransition.StateTransitionCircuit:
			placeholder = teststatetransition.CircuitPlaceholder()
		default:
			t.Fatalf("unsupported circuit type: %T", assignment)

		}

		// First run the circuit solver verification for debugging
		// The circuit itself is used as both assignment and placeholder
		// since it already contains all the witness values
		assert := test.NewAssert(t)

		assert.SolvingSucceeded(placeholder, assignment,
			test.WithCurves(curve),
			test.WithBackends(backend.GROTH16),
			test.WithProverOpts(opts...),
		)

		// Then do the normal proof generation
		witness, err := frontend.NewWitness(assignment, curve.ScalarField())
		if err != nil {
			return nil, fmt.Errorf("failed to create witness: %w", err)
		}

		// Generate the proof
		return groth16.Prove(ccs, pk, witness, opts...)
	}
}

// SetProver sets a custom prover function for the Sequencer.
// This is particularly useful for tests that need to debug circuit execution.
func (s *Sequencer) SetProver(p ProverFunc) {
	s.prover = p
}
