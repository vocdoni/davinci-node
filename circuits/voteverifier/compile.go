package voteverifier

import (
	"fmt"

	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/util/circomgnark"
)

// Compile compiles the VoteVerifier circuit definition using the canonical
// Circom placeholder setup.
func Compile() (constraint.ConstraintSystem, error) {
	circomPlaceholder, err := circomgnark.Circom2GnarkPlaceholder(
		ballotproof.CircomVerificationKey, circuits.BallotProofNPubInputs)
	if err != nil {
		return nil, fmt.Errorf("generate circom2gnark placeholder: %w", err)
	}
	ccs, err := frontend.Compile(params.VoteVerifierCurve.ScalarField(), r1cs.NewBuilder, &VerifyVoteCircuit{
		CircomVerificationKey: circomPlaceholder.Vk,
		CircomProof:           circomPlaceholder.Proof,
	})
	if err != nil {
		return nil, fmt.Errorf("compile vote verifier circuit: %w", err)
	}
	return ccs, nil
}
