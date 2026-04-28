package state

import (
	"fmt"
	"math/big"
	"slices"

	"github.com/vocdoni/arbo"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/blobs"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/spec/params"
)

// Batch owns the state transaction, counters, vote list, and witness data for
// a single staged vote batch.
type Batch struct {
	state *State
	tx    db.WriteTx

	committed bool
	discarded bool

	oldResults            *elgamal.Ballot
	newResults            *elgamal.Ballot
	allBallotsSum         *elgamal.Ballot
	overwrittenSum        *elgamal.Ballot
	votersCount           int
	overwrittenVotesCount int
	votes                 []*Vote

	rootHashBefore *big.Int
	rootHashAfter  *big.Int
	processProofs  ProcessProofs
	votesProofs    VotesProofs
	blobEvalData   *blobs.BlobEvalData
}

// PrepareVotesBatch stages a batch in a write transaction without committing it.
// Call Commit to persist the staged root, or Discard to discard it.
func (s *State) PrepareVotesBatch(votes []*Vote) (batch *Batch, err error) {
	batch = s.newBatch()
	defer func() {
		if err != nil {
			batch.Discard()
		}
	}()

	for _, v := range votes {
		if err := batch.addVote(v); err != nil {
			return nil, fmt.Errorf("failed to add vote: %w", err)
		}
	}
	if err := batch.prepareTransitions(); err != nil {
		return nil, fmt.Errorf("failed to prepare batch transitions: %w", err)
	}
	return batch, nil
}

func (s *State) newBatch() *Batch {
	return &Batch{
		state:                 s,
		tx:                    s.tree.WriteTx(),
		oldResults:            elgamal.NewBallot(Curve),
		newResults:            elgamal.NewBallot(Curve),
		allBallotsSum:         elgamal.NewBallot(Curve),
		overwrittenSum:        elgamal.NewBallot(Curve),
		votersCount:           0,
		overwrittenVotesCount: 0,
		votes:                 []*Vote{},
	}
}

// Commit commits the staged batch.
//
// Calling Commit more than once, or after Discard, is an error.
func (b *Batch) Commit() error {
	if b == nil {
		return fmt.Errorf("commit state batch: nil batch")
	}
	if b.tx == nil {
		if b.discarded {
			return fmt.Errorf("commit state batch: state batch was discarded")
		}
		return fmt.Errorf("commit state batch: no active state batch transaction")
	}
	if b.rootHashAfter == nil {
		rootHashAfter, err := b.RootAsBigInt()
		if err != nil {
			return fmt.Errorf("commit state batch: get staged root: %w", err)
		}
		b.rootHashAfter = rootHashAfter
	}
	tx := b.tx
	if err := tx.Commit(); err != nil {
		tx.Discard()
		b.tx = nil
		return fmt.Errorf("commit state batch: %w", err)
	}
	tx.Discard()
	b.tx = nil
	b.committed = true
	return nil
}

// Discard discards the staged batch transaction.
//
// This method can be safely called after any previous Commit or Discard call,
// for the sake of allowing deferred Discard calls.
func (b *Batch) Discard() {
	if b == nil || b.tx == nil {
		return
	}
	b.tx.Discard()
	b.tx = nil
	b.discarded = true
}

func (b *Batch) getBigInt(key *big.Int) (*big.Int, []*big.Int, error) {
	if b.tx == nil {
		return nil, nil, fmt.Errorf("state batch transaction is not active")
	}
	return b.state.tree.GetBigIntWithTx(b.tx, key)
}

func (b *Batch) addBigInt(key *big.Int, values ...*big.Int) error {
	if b.tx == nil {
		return fmt.Errorf("state batch transaction is not active")
	}
	return b.state.tree.AddBigIntWithTx(b.tx, key, values...)
}

func (b *Batch) updateBigInt(key *big.Int, values ...*big.Int) error {
	if b.tx == nil {
		return fmt.Errorf("state batch transaction is not active")
	}
	return b.state.tree.UpdateBigIntWithTx(b.tx, key, values...)
}

func (b *Batch) generateGnarkVerifierProofBigInt(key *big.Int) (*arbo.GnarkVerifierProof, error) {
	if b.tx == nil {
		return nil, fmt.Errorf("state batch transaction is not active")
	}
	return b.state.tree.GenerateGnarkVerifierProofBigIntWithTx(b.tx, key)
}

// RootAsBigInt returns the staged root while the transaction is active, or the
// captured after-root once the batch has been committed.
func (b *Batch) RootAsBigInt() (*big.Int, error) {
	if b == nil {
		return nil, fmt.Errorf("nil state batch")
	}
	if b.tx == nil {
		if b.discarded {
			return nil, fmt.Errorf("state batch was discarded")
		}
		if !b.committed {
			return nil, fmt.Errorf("state batch transaction is not active")
		}
		if b.rootHashAfter == nil {
			return nil, fmt.Errorf("state batch root hash after is not available")
		}
		return new(big.Int).Set(b.rootHashAfter), nil
	}
	root, err := b.state.tree.RootWithTx(b.tx)
	if err != nil {
		return nil, fmt.Errorf("get state batch root: %w", err)
	}
	return BytesToBigInt(root), nil
}

// Process returns all process details from the batch state.
func (b *Batch) Process() circuits.Process[*big.Int] { return b.state.Process() }

// VotersCount returns the number of voters participating in the batch, i.e.
// either casting their first vote or overwriting a previous one.
func (b *Batch) VotersCount() int { return b.votersCount }

// OldResults returns the old results ballot of the batch.
func (b *Batch) OldResults() *elgamal.Ballot { return b.oldResults }

// NewResults returns the new results ballot of the batch.
func (b *Batch) NewResults() *elgamal.Ballot { return b.newResults }

// OverwrittenVotesCount returns the number of ballots overwritten in the batch.
func (b *Batch) OverwrittenVotesCount() int { return b.overwrittenVotesCount }

// Votes returns the votes added in the batch.
func (b *Batch) Votes() []*Vote { return b.votes }

// PaddedVotes returns the votes added in the batch, padded to
// circuits.VotesPerBatch. The padding is done by adding empty votes with zero
// values.
func (b *Batch) PaddedVotes() []*Vote {
	votes := slices.Clone(b.votes)
	for len(votes) < params.VotesPerBatch {
		votes = append(votes, &Vote{
			Address:           big.NewInt(0),
			BallotIndex:       0,
			Ballot:            elgamal.NewBallot(Curve),
			ReencryptedBallot: elgamal.NewBallot(Curve),
			OverwrittenBallot: elgamal.NewBallot(Curve),
			Weight:            big.NewInt(0),
		})
	}
	return votes
}

// RootHashBefore returns the root hash before the state transition.
func (b *Batch) RootHashBefore() *big.Int { return new(big.Int).Set(b.rootHashBefore) }

// RootHashAfter returns the root hash after the state transition.
func (b *Batch) RootHashAfter() *big.Int { return new(big.Int).Set(b.rootHashAfter) }

// ProcessProofs returns the process proofs for the batch.
func (b *Batch) ProcessProofs() ProcessProofs { return b.processProofs }

// VotesProofs returns the votes proofs for the batch.
func (b *Batch) VotesProofs() VotesProofs { return b.votesProofs }

// BlobEvalData returns the cached blob evaluation data for the batch.
//
// blob layout:
//  1. Results (params.FieldsPerBallot * 4 coordinates)
//  2. VotersCount
//  3. Votes sequentially for exactly VotersCount entries:
//     Each vote: voteID + address + ballotIndex + weight + reencryptedBallot coordinates
func (b *Batch) BlobEvalData() *blobs.BlobEvalData { return b.blobEvalData }

// prepareTransitions generates the Merkle proofs for the current batch and
// stages the corresponding state tree mutations in the active transaction.
func (b *Batch) prepareTransitions() error {
	if b.tx == nil {
		return fmt.Errorf("need to start batch first")
	}
	var err error

	b.rootHashBefore, err = b.RootAsBigInt()
	if err != nil {
		return err
	}

	// first get MerkleProofs, since they need to belong to RootHashBefore, i.e.
	// before MerkleTransitions
	if b.processProofs.ID, err = b.GenArboProof(KeyProcessID); err != nil {
		return fmt.Errorf("could not get ID proof: %w", err)
	}
	if b.processProofs.CensusOrigin, err = b.GenArboProof(KeyCensusOrigin); err != nil {
		return fmt.Errorf("could not get CensusOrigin proof: %w", err)
	}
	if b.processProofs.BallotMode, err = b.GenArboProof(KeyBallotMode); err != nil {
		return fmt.Errorf("could not get BallotMode proof: %w", err)
	}
	if b.processProofs.EncryptionKey, err = b.GenArboProof(KeyEncryptionKey); err != nil {
		return fmt.Errorf("could not get EncryptionKey proof: %w", err)
	}

	// now build ordered chain of MerkleTransitions. The order should be the
	// same that the circuit will process them, so that the MerkleProofs are
	// in the same order as the MerkleTransitions
	for i := range b.votesProofs.Ballot {
		var errBallot, errVoteID error
		if i < len(b.Votes()) {
			v := b.Votes()[i]
			b.votesProofs.Ballot[i], errBallot = ArboTransitionFromAddOrUpdate(b,
				v.BallotIndex.StateKey(), v.TreeLeafValues()...)
			b.votesProofs.VoteID[i], errVoteID = ArboTransitionFromAddOrUpdate(b,
				v.VoteID.StateKey(), voteIDLeafValue)
		} else {
			b.votesProofs.Ballot[i], errBallot = ArboTransitionFromNoop(b)
			b.votesProofs.VoteID[i], errVoteID = ArboTransitionFromNoop(b)
		}
		if errBallot != nil {
			return fmt.Errorf("could not get Ballot proof for index %d: %w", i, errBallot)
		}
		if errVoteID != nil {
			return fmt.Errorf("could not get VoteID proof for index %d: %w", i, errVoteID)
		}
	}

	b.oldResults, err = resultsFromTree(b)
	if err != nil {
		return fmt.Errorf("results not found in state: %w", err)
	}
	b.newResults.Add(b.oldResults, b.allBallotsSum)
	b.newResults.Add(b.newResults, elgamal.NewBallot(Curve).Neg(b.overwrittenSum))
	b.votesProofs.Results, err = ArboTransitionFromAddOrUpdate(b, KeyResults, b.newResults.BigInts()...)
	if err != nil {
		return fmt.Errorf("results: %w", err)
	}

	b.rootHashAfter, err = b.RootAsBigInt()
	if err != nil {
		return fmt.Errorf("get root hash after: %w", err)
	}
	b.blobEvalData, err = b.computeBlobEvalData()
	if err != nil {
		return fmt.Errorf("blob eval data: %w", err)
	}

	return nil
}
