package sequencer

import (
	"context"
	"fmt"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/results"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
)

// internalCircuits holds the loaded circuit artifacts for the sequencer
// and is used to avoid loading them multiple times.
// It includes the vote verifier, aggregator, state transition, and results
// verifier circuits definitions and proving keys.
type internalCircuits struct {
	voteVerifier    *circuits.CircuitRuntime
	aggregator      *circuits.CircuitRuntime
	stateTransition *circuits.CircuitRuntime
	resultsVerifier *circuits.CircuitRuntime

	// voteVerifierDummyProof is used to FillWithDummy.
	voteVerifierDummyProof groth16.Proof
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

	s.voteVerifier, err = voteverifier.Artifacts.LoadOrDownload(context.Background())
	if err != nil {
		return fmt.Errorf("failed to load voteverifier artifacts: %w", err)
	}

	dummyAssignment, err := voteverifier.DummyAssignment()
	if err != nil {
		return fmt.Errorf("voteverifier dummy assignment error: %w", err)
	}

	dummyProof, err := s.voteVerifier.ProveAndVerify(dummyAssignment)
	if err != nil {
		return fmt.Errorf("failed to generate voteverifier dummy proof: %w", err)
	}

	s.voteVerifierDummyProof = dummyProof

	s.aggregator, err = aggregator.Artifacts.LoadOrDownload(context.Background())
	if err != nil {
		return fmt.Errorf("failed to load aggregator artifacts: %w", err)
	}

	s.stateTransition, err = statetransition.Artifacts.LoadOrDownload(context.Background())
	if err != nil {
		return fmt.Errorf("failed to load statetransition artifacts: %w", err)
	}

	s.resultsVerifier, err = results.Artifacts.LoadOrDownload(context.Background())
	if err != nil {
		return fmt.Errorf("failed to load resultsverifier artifacts: %w", err)
	}

	return nil
}
