package voteverifier

import (
	"fmt"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
)

// Prove method of VoteVerifierCircuit instance generates a proof of the
// validity values of the current assignment. It loads the required circuit
// artifacts and decodes them to the proper format. It returns the proof or an
// error.
func (assignment *VerifyVoteCircuit) Prove() (groth16.Proof, error) {
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
	// calculate the witness with the assignment
	witness, err := frontend.NewWitness(assignment, ecc.BLS12_377.ScalarField())
	if err != nil {
		return nil, fmt.Errorf("failed to create witness: %w", err)
	}
	// generate the final proof
	return groth16.Prove(ccs, pk, witness)
}
