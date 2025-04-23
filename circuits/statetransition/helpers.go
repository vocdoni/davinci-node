package statetransition

import (
	"fmt"

	"github.com/consensys/gnark/frontend"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/state"
)

// GenerateWitness generates the witness for the state transition circuit
// from the given state object. It populates the witness structure with
// the necessary data, including the root hash before and after the
// transition, the process information, the votes, and the results.
func GenerateWitness(o *state.State) (*StateTransitionCircuit, error) {
	var err error
	witness := &StateTransitionCircuit{}

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

	witness.ProcessProofs = ProcessProofs{}
	witness.ProcessProofs.ID, err = MerkleProofFromArboProof(o.ProcessProofs.ID)
	if err != nil {
		return nil, err
	}
	witness.ProcessProofs.CensusRoot, err = MerkleProofFromArboProof(o.ProcessProofs.CensusRoot)
	if err != nil {
		return nil, err
	}
	witness.ProcessProofs.BallotMode, err = MerkleProofFromArboProof(o.ProcessProofs.BallotMode)
	if err != nil {
		return nil, err
	}
	witness.ProcessProofs.EncryptionKey, err = MerkleProofFromArboProof(o.ProcessProofs.EncryptionKey)
	if err != nil {
		return nil, err
	}
	// add Ballots
	for i := range witness.VotesProofs.Ballot {
		witness.VotesProofs.Ballot[i], err = MerkleTransitionFromArboTransition(o.VotesProofs.Ballot[i])
		if err != nil {
			return nil, err
		}
	}
	// add Commitments
	for i := range witness.VotesProofs.Commitment {
		witness.VotesProofs.Commitment[i], err = MerkleTransitionFromArboTransition(o.VotesProofs.Commitment[i])
		if err != nil {
			return nil, err
		}
	}
	// update ResultsAdd
	witness.ResultsProofs.ResultsAdd, err = MerkleTransitionFromArboTransition(o.VotesProofs.ResultsAdd)
	if err != nil {
		return nil, fmt.Errorf("ResultsAdd: %w", err)
	}
	// update ResultsSub
	witness.ResultsProofs.ResultsSub, err = MerkleTransitionFromArboTransition(o.VotesProofs.ResultsSub)
	if err != nil {
		return nil, fmt.Errorf("ResultsSub: %w", err)
	}
	witness.Results = Results{
		OldResultsAdd: *o.OldResultsAdd().ToGnark(),
		OldResultsSub: *o.OldResultsSub().ToGnark(),
		NewResultsAdd: *o.NewResultsAdd().ToGnark(),
		NewResultsSub: *o.NewResultsSub().ToGnark(),
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
