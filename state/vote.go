package state

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/types"
)

var VoteIDKeyValue = big.NewInt(0)

// Vote describes a vote with homomorphic ballot
type Vote struct {
	Address           *big.Int
	VoteID            types.HexBytes
	Ballot            *elgamal.Ballot
	ReencryptedBallot *elgamal.Ballot // Reencrypted ballot for the state transition circuit
}

// SerializeBigInts returns
//
//	vote.Address
//	vote.Ballot
func (v *Vote) SerializeBigInts() []*big.Int {
	list := []*big.Int{}
	list = append(list, v.Address)
	list = append(list, v.Ballot.BigInts()...)
	return list
}

// AddVote adds a vote to the state
//   - if address exists, it counts as vote overwrite
func (o *State) AddVote(v *Vote) error {
	if o.dbTx == nil {
		return fmt.Errorf("need to StartBatch() first")
	}
	if len(o.votes) >= types.VotesPerBatch {
		return fmt.Errorf("too many votes for this batch")
	}
	// if address exists, it's a vote overwrite, need to count the overwritten
	// vote so it's later added to circuit.ResultsSub
	if _, value, err := o.tree.GetBigInt(v.Address); err == nil {
		oldVote, err := elgamal.NewBallot(Curve).SetBigInts(value)
		if err != nil {
			return err
		}
		o.overwrittenSum.Add(o.overwrittenSum, oldVote)
		o.overwrittenBallots = append(o.overwrittenBallots, oldVote)
		o.overwrittenCount++
	}
	o.ballotSum.Add(o.ballotSum, v.ReencryptedBallot)
	o.ballotCount++

	o.votes = append(o.votes, v)
	return nil
}

// EncryptedBallot returns the ballot associated with a address
func (o *State) EncryptedBallot(address *big.Int) (*elgamal.Ballot, error) {
	_, value, err := o.tree.GetBigInt(address)
	if err != nil {
		return nil, err
	}
	ballot, err := elgamal.NewBallot(Curve).SetBigInts(value)
	if err != nil {
		return nil, err
	}
	return ballot, nil
}

// ContainsVoteID checks if the state contains a vote ID
func (o *State) ContainsVoteID(voteID types.HexBytes) bool {
	_, _, err := o.tree.GetBigInt(voteID.BigInt().MathBigInt())
	return err == nil
}
