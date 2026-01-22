package state

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/vocdoni/arbo"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

var (
	// VoteIDKeyValue is a constant key used to store vote IDs in the state
	VoteIDKeyValue = big.NewInt(0)

	// ErrNotFound is returned when a key is not found in the state
	ErrKeyNotFound = fmt.Errorf("not found")
)

// Vote describes a vote with homomorphic ballot
type Vote struct {
	Address           *big.Int
	VoteID            types.VoteID
	Ballot            *elgamal.Ballot
	OverwrittenBallot *elgamal.Ballot
	Weight            *big.Int
	ReencryptedBallot *elgamal.Ballot // Reencrypted ballot for the state transition circuit
}

// AddVote adds a vote to the state.
// If v.Address exists already in the tree, it counts as vote overwrite.
// Note that this method modifies passed v, sets v.OverwrittenBallot
func (o *State) addVote(v *Vote) error {
	if o.dbTx == nil {
		return fmt.Errorf("need to StartBatch() first")
	}
	if len(o.votes) >= params.VotesPerBatch {
		return fmt.Errorf("too many votes for this batch")
	}
	// if address exists, it's a vote overwrite, need to count the overwritten
	// vote so it's later added to circuit.ResultsSub
	if oldVote, err := o.EncryptedBallot(types.CalculateBallotIndex(v.Address, types.IndexTODO)); err == nil {
		o.overwrittenSum.Add(o.overwrittenSum, oldVote)
		o.overwrittenVotesCount++
		v.OverwrittenBallot = oldVote
	} else {
		v.OverwrittenBallot = elgamal.NewBallot(Curve)
	}
	o.allBallotsSum.Add(o.allBallotsSum, v.ReencryptedBallot)
	o.votersCount++

	o.votes = append(o.votes, v)
	return nil
}

// EncryptedBallot returns the ballot associated with a address
func (o *State) EncryptedBallot(ballotIndex types.BallotIndex) (*elgamal.Ballot, error) {
	_, value, err := o.tree.GetBigInt(ballotIndex.BigInt())
	if err != nil {
		// Wrap arbo.ErrKeyNotFound to a specific error
		if errors.Is(err, arbo.ErrKeyNotFound) {
			return nil, ErrKeyNotFound
		}
		return nil, err
	}
	ballot, err := elgamal.NewBallot(Curve).SetBigInts(value)
	if err != nil {
		return nil, err
	}
	return ballot, nil
}

// ContainsVoteID checks if the state contains a vote ID
func (o *State) ContainsVoteID(voteID types.VoteID) bool {
	if !voteID.Valid() {
		log.Errorf("voteID %d is invalid", voteID)
		return false
	}
	_, _, err := o.tree.GetBigInt(voteID.BigInt())
	return err == nil
}

// ContainsBallot checks if the state contains an address
func (o *State) ContainsBallot(ballotIndex types.BallotIndex) bool {
	_, _, err := o.tree.GetBigInt(ballotIndex.BigInt())
	return err == nil
}

// HasAddressVoted checks if an address has voted in a given process. It opens
// the current process state and checks for the address. If found,
// it returns true, otherwise false. If there's an error opening the state or
// during the check, it returns the error.
func HasAddressVoted(db db.Database, processID types.ProcessID, ballotIndex types.BallotIndex) (bool, error) {
	s, err := New(db, processID)
	if err != nil {
		return false, fmt.Errorf("could not open state: %v", err)
	}
	return s.ContainsBallot(ballotIndex), nil
}
