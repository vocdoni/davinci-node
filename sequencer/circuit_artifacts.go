package sequencer

import (
	"fmt"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/results"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	ballotprooftest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/log"
)

// internalCircuits holds the loaded circuit artifacts for the sequencer
// and is used to avoid loading them multiple times.
// It includes the vote verifier, aggregator, state transition, and results
// verifier circuits definitions and proving keys, as well as the circom
// verification key for the ballot proof.
type internalCircuits struct {
	bVkCircom                   []byte
	vvCcs, aggCcs, stCcs, rvCcs constraint.ConstraintSystem
	vvPk, aggPk, stPk, rvPk     groth16.ProvingKey
	vvVk, stVk                  groth16.VerifyingKey
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
	// Load circom verification key for ballot proof
	// TODO: replace with the production verification key
	s.bVkCircom = ballotprooftest.TestCircomVerificationKey
	// Load vote verifier definition and proving key
	log.Debugw("reading ccs and pk cicuit artifact", "circuit", "voteVerifier")
	s.vvCcs, s.vvPk, err = loadCircuitArtifacts(voteverifier.Artifacts)
	if err != nil {
		return fmt.Errorf("failed to load vote verifier artifacts: %w", err)
	}

	// Load vote verifier verifying key
	s.vvVk, err = voteverifier.Artifacts.VerifyingKey()
	if err != nil {
		return fmt.Errorf("failed to load vote verifier verifying key: %w", err)
	}

	// Load aggregator artifacts
	log.Debugw("reading ccs and pk cicuit artifact", "circuit", "aggregator")
	s.aggCcs, s.aggPk, err = loadCircuitArtifacts(aggregator.Artifacts)
	if err != nil {
		return fmt.Errorf("failed to load aggregator artifacts: %w", err)
	}

	// Load statetransition artifacts
	log.Debugw("reading ccs and pk cicuit artifact", "circuit", "statetransition")
	s.stCcs, s.stPk, err = loadCircuitArtifacts(statetransition.Artifacts)
	if err != nil {
		return fmt.Errorf("failed to load statetransition artifacts: %w", err)
	}

	// Load resultsverifier artifacts
	log.Debugw("reading ccs and pk cicuit artifact", "circuit", "resultsverifier")
	s.rvCcs, s.rvPk, err = loadCircuitArtifacts(results.Artifacts)
	if err != nil {
		return fmt.Errorf("failed to load resultsverifier artifacts: %w", err)
	}

	// Load statetransition verifying key
	s.stVk, err = statetransition.Artifacts.VerifyingKey()
	if err != nil {
		return fmt.Errorf("failed to load statetransition verifying key: %w", err)
	}

	return nil
}

// loadCircuitArtifacts helper loads the files of the circuit artifacts
// provided and returns the decoded constraint system and proving key. If
// any of the files fail to load or decode, it returns an error.
func loadCircuitArtifacts(a *circuits.CircuitArtifacts) (constraint.ConstraintSystem, groth16.ProvingKey, error) {
	// Load the circuit artifacts
	if err := a.LoadAll(); err != nil {
		return nil, nil, fmt.Errorf("failed to load circuit artifacts: %w", err)
	}
	// Decode the circuit definition
	ccs, err := a.CircuitDefinition()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read circuit definition: %w", err)
	}
	// Decode the proving key
	pk, err := a.ProvingKey()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read proving key: %w", err)
	}
	return ccs, pk, nil
}
