package statetransitiontest

import (
	"fmt"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	"github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"

	"github.com/vocdoni/vocdoni-z-sandbox/circuits/statetransition"
	"github.com/vocdoni/vocdoni-z-sandbox/state"
)

func GenerateWitness(o *state.State) (*statetransition.Circuit, error) {
	var err error
	witness := &statetransition.Circuit{}

	// RootHashBefore
	witness.RootHashBefore = o.RootHashBefore

	witness.Process.ID = o.Process.ID
	witness.Process.CensusRoot = o.Process.CensusRoot
	witness.Process.BallotMode = circuits.BallotMode[frontend.Variable]{
		MaxCount:        o.Process.BallotMode.MaxCount,
		ForceUniqueness: o.Process.BallotMode.ForceUniqueness,
		MaxValue:        o.Process.BallotMode.MaxValue,
		MinValue:        o.Process.BallotMode.MinValue,
		MaxTotalCost:    o.Process.BallotMode.MaxTotalCost,
		MinTotalCost:    o.Process.BallotMode.MinTotalCost,
		CostExp:         o.Process.BallotMode.CostExp,
		CostFromWeight:  o.Process.BallotMode.CostFromWeight,
	}
	witness.Process.EncryptionKey.PubKey[0] = o.Process.EncryptionKey.PubKey[0]
	witness.Process.EncryptionKey.PubKey[1] = o.Process.EncryptionKey.PubKey[1]

	for i, v := range o.PaddedVotes() {
		witness.Votes[i].Nullifier = v.Nullifier
		witness.Votes[i].Ballot = *v.Ballot.ToGnark()
		witness.Votes[i].Address = v.Address
		witness.Votes[i].Commitment = v.Commitment
		witness.Votes[i].OverwrittenBallot = *o.OverwrittenBallots()[i].ToGnark()
	}

	witness.ProcessProofs = statetransition.ProcessProofs{
		ID:            statetransition.MerkleProofFromArboProof(o.ProcessProofs.ID),
		CensusRoot:    statetransition.MerkleProofFromArboProof(o.ProcessProofs.CensusRoot),
		BallotMode:    statetransition.MerkleProofFromArboProof(o.ProcessProofs.BallotMode),
		EncryptionKey: statetransition.MerkleProofFromArboProof(o.ProcessProofs.EncryptionKey),
	}

	// add Ballots
	for i := range witness.VotesProofs.Ballot {
		witness.VotesProofs.Ballot[i], err = statetransition.MerkleTransitionFromArboTransition(o.VotesProofs.Ballot[i])
		if err != nil {
			return nil, err
		}
	}

	// add Commitments
	for i := range witness.VotesProofs.Commitment {
		witness.VotesProofs.Commitment[i], err = statetransition.MerkleTransitionFromArboTransition(o.VotesProofs.Commitment[i])
		if err != nil {
			return nil, err
		}
	}

	// update ResultsAdd
	witness.ResultsProofs.ResultsAdd, err = statetransition.MerkleTransitionFromArboTransition(o.VotesProofs.ResultsAdd)
	if err != nil {
		return nil, fmt.Errorf("ResultsAdd: %w", err)
	}

	// update ResultsSub
	witness.ResultsProofs.ResultsSub, err = statetransition.MerkleTransitionFromArboTransition(o.VotesProofs.ResultsSub)
	if err != nil {
		return nil, fmt.Errorf("ResultsSub: %w", err)
	}

	witness.Results = statetransition.Results{
		OldResultsAdd: *o.OldResultsAdd.ToGnark(),
		OldResultsSub: *o.OldResultsSub.ToGnark(),
		NewResultsAdd: *o.NewResultsAdd.ToGnark(),
		NewResultsSub: *o.NewResultsSub.ToGnark(),
	}

	// update stats
	witness.NumNewVotes = o.BallotCount()
	witness.NumOverwrites = o.OverwriteCount()
	// RootHashAfter
	witness.RootHashAfter, err = o.RootAsBigInt()
	if err != nil {
		return nil, err
	}

	return witness, nil
}

func CircuitPlaceholder() *statetransition.Circuit {
	proof, vk := DummyAggProofPlaceholder()
	return CircuitPlaceholderWithProof(proof, vk)
}

func CircuitPlaceholderWithProof(
	proof *groth16.Proof[sw_bw6761.G1Affine, sw_bw6761.G2Affine],
	vk *groth16.VerifyingKey[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl],
) *statetransition.Circuit {
	return &statetransition.Circuit{
		AggregatorProof: *proof,
		AggregatorVK:    *vk,
	}
}
