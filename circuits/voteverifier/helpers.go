package voteverifier

import (
	"fmt"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/davinci-node/spec/params"
)

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
