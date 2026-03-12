package statetransition

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/circuits/merkleproof"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

// PublicInputs contains all the public inputs for the state transition circuit
// in their original format (not Gnark format). This is useful for tests and
// for creating the storage.StateTransitionBatchProofInputs.
type PublicInputs struct {
	RootHashBefore        *big.Int
	RootHashAfter         *big.Int
	VotersCount           *big.Int
	OverwrittenVotesCount *big.Int
	CensusRoot            *big.Int
	BlobCommitmentLimbs   [3]*big.Int
}

// GenerateAssignment builds the circuit assignment for the state transition circuit
// from the given state object. It populates the assignment structure with
// the necessary data, including the root hash before and after the
// transition, the process information, the votes, and the results.
// It also returns the public inputs in their original format.
func GenerateAssignment(
	o *state.State,
	censusRoot *types.BigInt,
	censusProofs CensusProofs,
	kSeed *types.BigInt,
) (*StateTransitionCircuit, *PublicInputs, error) {
	var err error
	assignment := &StateTransitionCircuit{
		CensusRoot:   censusRoot.MathBigInt(),
		CensusProofs: censusProofs,
	}

	// Include the k used for re-encryption
	assignment.ReencryptionK = kSeed.MathBigInt()

	// RootHashBefore
	assignment.RootHashBefore = o.RootHashBefore()

	// Process info
	assignment.Process.ID = o.Process().ID
	assignment.Process.CensusOrigin = o.Process().CensusOrigin
	assignment.Process.BallotMode = o.Process().BallotMode
	assignment.Process.EncryptionKey.PubKey[0] = o.Process().EncryptionKey.PubKey[0]
	assignment.Process.EncryptionKey.PubKey[1] = o.Process().EncryptionKey.PubKey[1]

	// update stats
	assignment.VotersCount = o.VotersCount()
	assignment.OverwrittenVotesCount = o.OverwrittenVotesCount()

	for i, v := range o.PaddedVotes() {
		assignment.Votes[i].Ballot = *v.Ballot.ToGnark()
		assignment.Votes[i].ReencryptedBallot = *v.ReencryptedBallot.ToGnark()
		assignment.Votes[i].Address = v.Address
		assignment.Votes[i].BallotIndex = v.BallotIndex.Uint64()
		assignment.Votes[i].VoteWeight = v.Weight
		assignment.Votes[i].VoteID = v.VoteID.Uint64()
		assignment.Votes[i].OverwrittenBallot = *v.OverwrittenBallot.ToGnark()
	}

	assignment.ProcessProofs = ProcessProofs{}
	assignment.ProcessProofs.ID, err = merkleproof.MerkleProofFromArboProof(o.ProcessProofs().ID)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get ID proof: %w", err)
	}
	assignment.ProcessProofs.CensusOrigin, err = merkleproof.MerkleProofFromArboProof(o.ProcessProofs().CensusOrigin)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get CensusOrigin proof: %w", err)
	}
	assignment.ProcessProofs.BallotMode, err = merkleproof.MerkleProofFromArboProof(o.ProcessProofs().BallotMode)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get BallotMode proof: %w", err)
	}
	assignment.ProcessProofs.EncryptionKey, err = merkleproof.MerkleProofFromArboProof(o.ProcessProofs().EncryptionKey)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get EncryptionKey proof: %w", err)
	}
	// add Ballots and VoteIDs proofs
	for i := range params.VotesPerBatch {
		// ballots
		assignment.VotesProofs.Ballot[i], err = merkleproof.MerkleTransitionFromArboTransition(o.VotesProofs().Ballot[i])
		if err != nil {
			return nil, nil, fmt.Errorf("could not get Ballot proof for index %d: %w", i, err)
		}
		// vote IDs
		assignment.VotesProofs.VoteIDs[i], err = merkleproof.MerkleTransitionFromArboTransition(o.VotesProofs().VoteID[i])
		if err != nil {
			return nil, nil, fmt.Errorf("could not get VoteID proof for index %d: %w", i, err)
		}
	}
	// update ResultsAdd
	assignment.ResultsProofs.ResultsAdd, err = merkleproof.MerkleTransitionFromArboTransition(o.VotesProofs().ResultsAdd)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get ResultsAdd proof: %w", err)
	}
	// update ResultsSub
	assignment.ResultsProofs.ResultsSub, err = merkleproof.MerkleTransitionFromArboTransition(o.VotesProofs().ResultsSub)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get ResultsSub proof: %w", err)
	}
	assignment.Results = Results{
		OldResultsAdd: *o.OldResultsAdd().ToGnark(),
		OldResultsSub: *o.OldResultsSub().ToGnark(),
		NewResultsAdd: *o.NewResultsAdd().ToGnark(),
		NewResultsSub: *o.NewResultsSub().ToGnark(),
	}
	// RootHashAfter
	assignment.RootHashAfter, err = o.RootAsBigInt()
	if err != nil {
		return nil, nil, err
	}

	// Blob evaluation data
	blobData, err := o.BlobEvalData()
	if err != nil {
		return nil, nil, fmt.Errorf("could not get blob eval data: %w", err)
	}

	// Assign commitment and proof limbs to the circuit assignment.
	assignment.BlobCommitmentLimbs = blobData.ForGnark.CommitmentLimbs
	assignment.BlobProofLimbs = blobData.ForGnark.ProofLimbs
	assignment.BlobEvaluationResultY = blobData.ForGnark.Y

	// Create public inputs in original format
	// Convert frontend.Variable to *big.Int properly
	var votersCount, overwrittenVotesCount *big.Int

	// Handle VotersCount conversion
	switch v := assignment.VotersCount.(type) {
	case *big.Int:
		votersCount = v
	case int:
		votersCount = big.NewInt(int64(v))
	default:
		return nil, nil, fmt.Errorf("unexpected type for VotersCount: %T", v)
	}

	// Handle OverwrittenVotesCount conversion
	switch v := assignment.OverwrittenVotesCount.(type) {
	case *big.Int:
		overwrittenVotesCount = v
	case int:
		overwrittenVotesCount = big.NewInt(int64(v))
	default:
		return nil, nil, fmt.Errorf("unexpected type for OverwrittenVotesCount: %T", v)
	}

	publicInputs := &PublicInputs{
		RootHashBefore:        o.RootHashBefore(),
		RootHashAfter:         assignment.RootHashAfter.(*big.Int),
		VotersCount:           votersCount,
		OverwrittenVotesCount: overwrittenVotesCount,
		CensusRoot:            censusRoot.MathBigInt(),
		BlobCommitmentLimbs:   blobData.CommitmentLimbs,
	}

	return assignment, publicInputs, nil
}
