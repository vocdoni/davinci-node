package statetransition

import (
	"fmt"

	"github.com/consensys/gnark/frontend"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/merkleproof"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

// GenerateWitness generates the witness for the state transition circuit
// from the given state object. It populates the witness structure with
// the necessary data, including the root hash before and after the
// transition, the process information, the votes, and the results.
func GenerateWitness(o *state.State, kSeed *types.BigInt) (*StateTransitionCircuit, error) {
	var err error
	witness := &StateTransitionCircuit{}

	// Include the k used for re-encryption
	witness.ReencryptionK = kSeed.MathBigInt()
	// RootHashBefore
	witness.RootHashBefore = o.RootHashBefore()

	witness.Process.ID = o.Process().ID
	witness.Process.CensusOrigin = o.Process().CensusOrigin
	witness.Process.CensusRoot = o.Process().CensusRoot
	witness.Process.BallotMode = circuits.BallotMode[frontend.Variable]{
		NumFields:      o.Process().BallotMode.NumFields,
		UniqueValues:   o.Process().BallotMode.UniqueValues,
		MaxValue:       o.Process().BallotMode.MaxValue,
		MinValue:       o.Process().BallotMode.MinValue,
		MaxValueSum:    o.Process().BallotMode.MaxValueSum,
		MinValueSum:    o.Process().BallotMode.MinValueSum,
		CostExponent:   o.Process().BallotMode.CostExponent,
		CostFromWeight: o.Process().BallotMode.CostFromWeight,
	}
	witness.Process.EncryptionKey.PubKey[0] = o.Process().EncryptionKey.PubKey[0]
	witness.Process.EncryptionKey.PubKey[1] = o.Process().EncryptionKey.PubKey[1]

	// update stats
	witness.NumNewVotes = o.BallotCount()
	witness.NumOverwritten = o.OverwrittenCount()

	for i, v := range o.PaddedVotes() {
		witness.Votes[i].Ballot = *v.Ballot.ToGnark()
		witness.Votes[i].ReencryptedBallot = *v.ReencryptedBallot.ToGnark()
		witness.Votes[i].Address = v.Address
		witness.Votes[i].VoteID = v.VoteID.BigInt().MathBigInt()
		witness.Votes[i].OverwrittenBallot = *o.OverwrittenBallots()[i].ToGnark()
	}

	witness.ProcessProofs = ProcessProofs{}
	witness.ProcessProofs.ID, err = merkleproof.MerkleProofFromArboProof(o.ProcessProofs().ID)
	if err != nil {
		return nil, fmt.Errorf("could not get ID proof: %w", err)
	}
	witness.ProcessProofs.CensusOrigin, err = merkleproof.MerkleProofFromArboProof(o.ProcessProofs().CensusOrigin)
	if err != nil {
		return nil, fmt.Errorf("could not get CensusOrigin proof: %w", err)
	}
	witness.ProcessProofs.CensusRoot, err = merkleproof.MerkleProofFromArboProof(o.ProcessProofs().CensusRoot)
	if err != nil {
		return nil, fmt.Errorf("could not get CensusRoot proof: %w", err)
	}
	witness.ProcessProofs.BallotMode, err = merkleproof.MerkleProofFromArboProof(o.ProcessProofs().BallotMode)
	if err != nil {
		return nil, fmt.Errorf("could not get BallotMode proof: %w", err)
	}
	witness.ProcessProofs.EncryptionKey, err = merkleproof.MerkleProofFromArboProof(o.ProcessProofs().EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("could not get EncryptionKey proof: %w", err)
	}
	// add Ballots and VoteIDs proofs
	for i := range witness.VotesProofs.Ballot {
		// ballots
		witness.VotesProofs.Ballot[i], err = merkleproof.MerkleTransitionFromArboTransition(o.VotesProofs().Ballot[i])
		if err != nil {
			return nil, fmt.Errorf("could not get Ballot proof for index %d: %w", i, err)
		}
		// vote IDs
		witness.VotesProofs.VoteIDs[i], err = merkleproof.MerkleTransitionFromArboTransition(o.VotesProofs().VoteID[i])
		if err != nil {
			return nil, fmt.Errorf("could not get VoteID proof for index %d: %w", i, err)
		}
	}
	// update ResultsAdd
	witness.ResultsProofs.ResultsAdd, err = merkleproof.MerkleTransitionFromArboTransition(o.VotesProofs().ResultsAdd)
	if err != nil {
		return nil, fmt.Errorf("could not get ResultsAdd proof: %w", err)
	}
	// update ResultsSub
	witness.ResultsProofs.ResultsSub, err = merkleproof.MerkleTransitionFromArboTransition(o.VotesProofs().ResultsSub)
	if err != nil {
		return nil, fmt.Errorf("could not get ResultsSub proof: %w", err)
	}
	witness.Results = Results{
		OldResultsAdd: *o.OldResultsAdd().ToGnark(),
		OldResultsSub: *o.OldResultsSub().ToGnark(),
		NewResultsAdd: *o.NewResultsAdd().ToGnark(),
		NewResultsSub: *o.NewResultsSub().ToGnark(),
	}
	// RootHashAfter
	witness.RootHashAfter, err = o.RootAsBigInt()
	if err != nil {
		return nil, err
	}

	// Blob evaluation data
	blobData, err := o.BuildKZGCommitment()
	if err != nil {
		return nil, fmt.Errorf("could not build KZG commitment: %w", err)
	}
	witness.BlobEvaluationPointZ = blobData.ForGnark.Z
	witness.BlobEvaluationResultY = blobData.ForGnark.Y

	return witness, nil
}
