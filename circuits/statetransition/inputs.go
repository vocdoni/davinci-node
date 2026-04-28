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
// from the given staged batch. It populates the assignment structure with
// the necessary data, including the root hash before and after the
// transition, the process information, the votes, and the results.
// It also returns the public inputs in their original format.
func GenerateAssignment(
	batch *state.Batch,
	censusRoot *types.BigInt,
	censusProofs CensusProofs,
	kSeed *types.BigInt,
) (*StateTransitionCircuit, *PublicInputs, error) {
	var err error

	blobData := batch.BlobEvalData()

	assignment := &StateTransitionCircuit{
		CensusRoot:    censusRoot.MathBigInt(),
		CensusProofs:  censusProofs,
		ReencryptionK: kSeed.MathBigInt(),

		RootHashBefore: batch.RootHashBefore(),
		RootHashAfter:  batch.RootHashAfter(),

		Process: batch.Process().ToGnark(),

		VotersCount:           batch.VotersCount(),
		OverwrittenVotesCount: batch.OverwrittenVotesCount(),

		Results: Results{
			OldResults: *batch.OldResults().ToGnark(),
			NewResults: *batch.NewResults().ToGnark(),
		},

		BlobCommitmentLimbs:   blobData.ForGnark.CommitmentLimbs,
		BlobProofLimbs:        blobData.ForGnark.ProofLimbs,
		BlobEvaluationResultY: blobData.ForGnark.Y,
	}

	for i, v := range batch.PaddedVotes() {
		assignment.Votes[i].Ballot = *v.Ballot.ToGnark()
		assignment.Votes[i].ReencryptedBallot = *v.ReencryptedBallot.ToGnark()
		assignment.Votes[i].Address = v.Address
		assignment.Votes[i].BallotIndex = v.BallotIndex.Uint64()
		assignment.Votes[i].VoteWeight = v.Weight
		assignment.Votes[i].VoteID = v.VoteID.Uint64()
		assignment.Votes[i].OverwrittenBallot = *v.OverwrittenBallot.ToGnark()
	}

	assignment.ProcessProofs = ProcessProofs{}
	assignment.ProcessProofs.ID, err = merkleproof.MerkleProofFromArboProof(batch.ProcessProofs().ID)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get ID proof: %w", err)
	}
	assignment.ProcessProofs.CensusOrigin, err = merkleproof.MerkleProofFromArboProof(batch.ProcessProofs().CensusOrigin)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get CensusOrigin proof: %w", err)
	}
	assignment.ProcessProofs.BallotMode, err = merkleproof.MerkleProofFromArboProof(batch.ProcessProofs().BallotMode)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get BallotMode proof: %w", err)
	}
	assignment.ProcessProofs.EncryptionKey, err = merkleproof.MerkleProofFromArboProof(batch.ProcessProofs().EncryptionKey)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get EncryptionKey proof: %w", err)
	}

	for i := range params.VotesPerBatch {
		assignment.VotesProofs.Ballot[i], err = merkleproof.MerkleTransitionFromArboTransition(batch.VotesProofs().Ballot[i])
		if err != nil {
			return nil, nil, fmt.Errorf("could not get Ballot proof for index %d: %w", i, err)
		}

		assignment.VotesProofs.VoteIDs[i], err = merkleproof.MerkleTransitionFromArboTransition(batch.VotesProofs().VoteID[i])
		if err != nil {
			return nil, nil, fmt.Errorf("could not get VoteID proof for index %d: %w", i, err)
		}
	}

	assignment.ResultsProofs.Results, err = merkleproof.MerkleTransitionFromArboTransition(batch.VotesProofs().Results)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get Results proof: %w", err)
	}

	publicInputs := &PublicInputs{
		RootHashBefore:        batch.RootHashBefore(),
		RootHashAfter:         batch.RootHashAfter(),
		VotersCount:           big.NewInt(int64(batch.VotersCount())),
		OverwrittenVotesCount: big.NewInt(int64(batch.OverwrittenVotesCount())),
		CensusRoot:            censusRoot.MathBigInt(),
		BlobCommitmentLimbs:   blobData.CommitmentLimbs,
	}

	return assignment, publicInputs, nil
}
