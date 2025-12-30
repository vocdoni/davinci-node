package state

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/vocdoni/arbo"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
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
	VoteID            types.HexBytes
	Ballot            *elgamal.Ballot
	OverwrittenBallot *elgamal.Ballot
	Weight            *big.Int
	ReencryptedBallot *elgamal.Ballot // Reencrypted ballot for the state transition circuit
}

// SerializeBigInts returns
//   - vote.Address
//   - vote.VoteID
//   - vote.UserWeight
//   - vote.Ballot
func (v *Vote) SerializeBigInts() []*big.Int {
	list := []*big.Int{}
	list = append(list, v.Address)
	list = append(list, v.VoteID.BigInt().MathBigInt())
	list = append(list, v.Weight)
	list = append(list, v.Ballot.BigInts()...)
	return list
}

// AddVote adds a vote to the state.
// If v.Address exists already in the tree, it counts as vote overwrite.
// Note that this method modifies passed v, sets v.OverwrittenBallot
func (o *State) AddVote(v *Vote) error {
	if o.dbTx == nil {
		return fmt.Errorf("need to StartBatch() first")
	}
	if len(o.votes) >= params.VotesPerBatch {
		return fmt.Errorf("too many votes for this batch")
	}
	if keyIsBelowReservedOffset(v.Address) {
		return fmt.Errorf("vote address %d is below the reserved offset", v.Address)
	}
	// if address exists, it's a vote overwrite, need to count the overwritten
	// vote so it's later added to circuit.ResultsSub
	if oldVote, err := o.EncryptedBallot(v.Address); err == nil {
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
func (o *State) EncryptedBallot(address *big.Int) (*elgamal.Ballot, error) {
	if keyIsBelowReservedOffset(address) {
		return nil, fmt.Errorf("vote address %d is below the reserved offset", address)
	}
	_, value, err := o.tree.GetBigInt(address)
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
func (o *State) ContainsVoteID(voteID *big.Int) bool {
	if keyIsBelowReservedOffset(voteID) {
		log.Errorf("voteID %d is below the reserved offset", voteID)
		return false
	}
	_, _, err := o.tree.GetBigInt(voteID)
	return err == nil
}

// ContainsAddress checks if the state contains an address
func (o *State) ContainsAddress(address *types.BigInt) bool {
	_, _, err := o.tree.GetBigInt(address.MathBigInt())
	return err == nil
}

// HasAddressVoted checks if an address has voted in a given process. It opens
// the state at the process's state root and checks for the address. If found,
// it returns true, otherwise false. If there's an error opening the state or
// during the check, it returns the error.
func HasAddressVoted(db db.Database, pid types.HexBytes, stateRoot, address *types.BigInt) (bool, error) {
	s, err := LoadOnRoot(db, pid.BigInt().MathBigInt(), stateRoot.MathBigInt())
	if err != nil {
		return false, fmt.Errorf("could not open state: %v", err)
	}
	return s.ContainsAddress(address), nil
}
