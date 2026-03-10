package sequencer

import (
	"fmt"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/results"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/log"
)

type nativeCircuitArtifacts struct {
	ccs constraint.ConstraintSystem
	pk  groth16.ProvingKey
	vk  groth16.VerifyingKey
}

// internalCircuits holds the loaded circuit artifacts for the sequencer
// and is used to avoid loading them multiple times.
// It includes the vote verifier, aggregator, state transition, and results
// verifier circuits definitions and proving keys, as well as the circom
// verification key for the ballot proof.
type internalCircuits struct {
	ballotProofVK   []byte
	voteVerifier    nativeCircuitArtifacts
	aggregator      nativeCircuitArtifacts
	stateTransition nativeCircuitArtifacts
	resultsVerifier nativeCircuitArtifacts
}

// loadInternalCircuitArtifacts loads the internal circuit artifacts for the
// sequencer. It initializes the following circuits:
//
//   - Vote Verifier
//   - Aggregator
//   - State Transition
//   - Results Verifier
//
// Including their constraint systems and proving keys.
// It returns an error if any of the artifacts fail to load.
func (s *Sequencer) loadInternalCircuitArtifacts() error {
	var err error
	s.internalCircuits = new(internalCircuits)

	s.ballotProofVK = ballotproof.CircomVerificationKey

	log.Debugw("reading circuit artifacts", "circuit", "voteverifier")
	s.voteVerifier, err = loadCircuitArtifacts(voteverifier.Artifacts)
	if err != nil {
		return fmt.Errorf("failed to load voteverifier artifacts: %w", err)
	}

	log.Debugw("reading circuit artifacts", "circuit", "aggregator")
	s.aggregator, err = loadCircuitArtifacts(aggregator.Artifacts)
	if err != nil {
		return fmt.Errorf("failed to load aggregator artifacts: %w", err)
	}

	log.Debugw("reading circuit artifacts", "circuit", "statetransition")
	s.stateTransition, err = loadCircuitArtifacts(statetransition.Artifacts)
	if err != nil {
		return fmt.Errorf("failed to load statetransition artifacts: %w", err)
	}

	log.Debugw("reading circuit artifacts", "circuit", "resultsverifier")
	s.resultsVerifier, err = loadCircuitArtifacts(results.Artifacts)
	if err != nil {
		return fmt.Errorf("failed to load resultsverifier artifacts: %w", err)
	}

	return nil
}

// loadCircuitArtifacts helper loads the files of the circuit artifacts
// provided and returns the decoded runtime artifacts. If any of the files fail
// to load or decode, it returns an error.
func loadCircuitArtifacts(a *circuits.CircuitArtifacts) (nativeCircuitArtifacts, error) {
	// Load the circuit artifacts
	if err := a.LoadAll(); err != nil {
		return nativeCircuitArtifacts{}, fmt.Errorf("failed to load circuit artifacts: %w", err)
	}
	// Decode the circuit definition
	ccs, err := a.CircuitDefinition()
	if err != nil {
		return nativeCircuitArtifacts{}, fmt.Errorf("failed to read circuit definition: %w", err)
	}
	// Decode the proving key
	pk, err := a.ProvingKey()
	if err != nil {
		return nativeCircuitArtifacts{}, fmt.Errorf("failed to read proving key: %w", err)
	}
	// Decode the verifying key
	vk, err := a.VerifyingKey()
	if err != nil {
		return nativeCircuitArtifacts{}, fmt.Errorf("failed to read verifying key: %w", err)
	}
	return nativeCircuitArtifacts{
		ccs: ccs,
		pk:  pk,
		vk:  vk,
	}, nil
}
