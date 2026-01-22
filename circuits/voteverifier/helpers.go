package voteverifier

import (
	"fmt"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/spec/params"
)

// Prove method of VoteVerifierCircuit instance generates a proof of the
// validity values of the current assignment. It loads the required circuit
// artifacts and decodes them to the proper format. It returns the proof or an
// error.
func (a *VerifyVoteCircuit) Prove() (groth16.Proof, error) {
	// load circuit artifacts content
	if err := Artifacts.LoadAll(); err != nil {
		return nil, fmt.Errorf("failed to load vote verifier artifacts: %w", err)
	}
	// decode the circuit definition (constrain system)
	ccs, err := Artifacts.CircuitDefinition()
	if err != nil {
		return nil, fmt.Errorf("failed to read vote verifier definition: %w", err)
	}
	// decode the proving key
	pk, err := Artifacts.ProvingKey()
	if err != nil {
		return nil, fmt.Errorf("failed to read vote verifier proving key: %w", err)
	}
	// generate the final proof using the default prover (supports GPU if enabled)
	return prover.DefaultProver(params.VoteVerifierCurve, ccs, pk, a)
}

// VerifyProof method verifies the proof of the circuit with the current
// assignment. It loads the verifying key from circuit artifacts, encodes the
// witness and tries to verify the given proof. It is usefull to validate the
// proofs before include them in a batch for recursion. If something fails
// return an error.
func (a *VerifyVoteCircuit) VerifyProof(proof groth16.Proof) error {
	// load circuit artifacts content
	if err := Artifacts.LoadAll(); err != nil {
		return fmt.Errorf("failed to load vote verifier artifacts: %w", err)
	}
	// load verifying key
	vk, err := Artifacts.VerifyingKey()
	if err != nil {
		return fmt.Errorf("failed to read vote verifier verifying key: %w", err)
	}
	// encode the assignment to public witness
	pubWitness, err := frontend.NewWitness(a, params.VoteVerifierCurve.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return fmt.Errorf("failed to create witness: %w", err)
	}
	// set up the verifier for the circuit curves
	opts := stdgroth16.GetNativeVerifierOptions(
		params.AggregatorCurve.ScalarField(),
		params.VoteVerifierCurve.ScalarField(),
	)
	// verify the proof
	if err := groth16.Verify(proof, vk, pubWitness, opts); err != nil {
		return fmt.Errorf("failed to verify proof: %w", err)
	}
	return nil
}
