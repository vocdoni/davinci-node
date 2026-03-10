package sequencer

import (
	"fmt"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/solidity"
	"github.com/consensys/gnark/frontend"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/results"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/spec/params"
)

func (s *Sequencer) verifyVoteVerifierProof(proof groth16.Proof, assignment *voteverifier.VerifyVoteCircuit) error {
	pubWitness, err := frontend.NewWitness(assignment, params.VoteVerifierCurve.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return fmt.Errorf("failed to create public witness: %w", err)
	}
	verifyOpts := stdgroth16.GetNativeVerifierOptions(
		params.AggregatorCurve.ScalarField(),
		params.VoteVerifierCurve.ScalarField(),
	)
	if err := groth16.Verify(proof, s.voteVerifier.vk, pubWitness, verifyOpts); err != nil {
		return fmt.Errorf("failed to verify generated vote verifier proof: %w", err)
	}
	return nil
}

func (s *Sequencer) verifyAggregatorProof(proof groth16.Proof, assignment *aggregator.AggregatorCircuit) error {
	pubWitness, err := frontend.NewWitness(assignment, params.AggregatorCurve.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return fmt.Errorf("failed to create aggregator public witness: %w", err)
	}
	verifyOpts := stdgroth16.GetNativeVerifierOptions(
		params.StateTransitionCurve.ScalarField(),
		params.AggregatorCurve.ScalarField(),
	)
	if err := groth16.Verify(proof, s.aggregator.vk, pubWitness, verifyOpts); err != nil {
		return fmt.Errorf("failed to verify generated aggregate proof: %w", err)
	}
	return nil
}

func (s *Sequencer) verifyStateTransitionProof(proof groth16.Proof, assignment *statetransition.StateTransitionCircuit) error {
	pubWitness, err := frontend.NewWitness(assignment, params.StateTransitionCurve.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return fmt.Errorf("failed to create state transition public witness: %w", err)
	}
	verifyOpts := solidity.WithVerifierTargetSolidityVerifier(backend.GROTH16)
	if err := groth16.Verify(proof, s.stateTransition.vk, pubWitness, verifyOpts); err != nil {
		return fmt.Errorf("failed to verify generated state transition proof: %w", err)
	}
	return nil
}

func (c *internalCircuits) verifyResultsProof(proof groth16.Proof, assignment *results.ResultsVerifierCircuit) error {
	pubWitness, err := frontend.NewWitness(assignment, params.ResultsVerifierCurve.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return fmt.Errorf("failed to create results verifier public witness: %w", err)
	}
	verifyOpts := solidity.WithVerifierTargetSolidityVerifier(backend.GROTH16)
	if err := groth16.Verify(proof, c.resultsVerifier.vk, pubWitness, verifyOpts); err != nil {
		return fmt.Errorf("failed to verify generated results proof: %w", err)
	}
	return nil
}
