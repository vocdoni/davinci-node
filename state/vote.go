package state

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/arbo"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// Vote describes a vote with homomorphic ballot
type Vote struct {
	Address    []byte
	Commitment *big.Int
	Nullifier  []byte
	Ballot     *elgamal.Ballot
}

// SerializeBigInts returns
//
//	vote.Address
//	vote.Commitment
//	vote.Nullifier
//	vote.Ballot
func (v *Vote) SerializeBigInts() []*big.Int {
	list := []*big.Int{}
	list = append(list, arbo.BytesToBigInt(v.Address))
	list = append(list, v.Commitment)
	list = append(list, arbo.BytesToBigInt(v.Nullifier))
	list = append(list, v.Ballot.BigInts()...)
	return list
}

// AddVote adds a vote to the state
//   - if nullifier exists, it counts as vote overwrite
func (o *State) AddVote(v *Vote) error {
	if o.dbTx == nil {
		return fmt.Errorf("need to StartBatch() first")
	}
	if len(o.votes) >= types.VotesPerBatch {
		return fmt.Errorf("too many votes for this batch")
	}

	// if nullifier exists, it's a vote overwrite, need to count the overwritten vote
	// so it's later added to circuit.ResultsSub
	if _, value, err := o.tree.Get(v.Nullifier); err == nil {
		oldVote := elgamal.NewBallot(Curve)
		if err := oldVote.Deserialize(value); err != nil {
			return err
		}
		o.OverwriteSum.Add(o.OverwriteSum, oldVote)
		o.overwrittenBallots = append(o.overwrittenBallots, oldVote)
		o.overwriteCount++
	}

	o.BallotSum.Add(o.BallotSum, v.Ballot)
	o.ballotCount++

	o.votes = append(o.votes, v)
	return nil
}
