package state

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/types"
)

// Vote describes a vote with homomorphic ballot
type Vote struct {
	Address    *big.Int
	Commitment *big.Int
	Nullifier  *big.Int
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
	list = append(list, v.Address)
	list = append(list, v.Commitment)
	list = append(list, v.Nullifier)
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
	// if nullifier exists, it's a vote overwrite, need to count the overwritten
	// vote so it's later added to circuit.ResultsSub
	if _, value, err := o.tree.GetBigInt(v.Nullifier); err == nil {
		oldVote, err := elgamal.NewBallot(Curve).SetBigInts(value)
		if err != nil {
			return err
		}
		o.overwriteSum.Add(o.overwriteSum, oldVote)
		o.overwrittenBallots = append(o.overwrittenBallots, oldVote)
		o.overwriteCount++
	}

	o.ballotSum.Add(o.ballotSum, v.Ballot)
	o.ballotCount++

	o.votes = append(o.votes, v)
	return nil
}

// EncryptedBallot returns the ballot associated with a nullifier
func (o *State) EncryptedBallot(nullifier *big.Int) (*elgamal.Ballot, error) {
	_, value, err := o.tree.GetBigInt(nullifier)
	if err != nil {
		return nil, err
	}
	ballot, err := elgamal.NewBallot(Curve).SetBigInts(value)
	if err != nil {
		return nil, err
	}
	return ballot, nil
}

// Commitment returns the commitment for a given address
func (o *State) Commitment(addressBytes types.HexBytes) (*big.Int, error) {
	address := common.BytesToAddress(addressBytes)
	_, value, err := o.tree.GetBigInt(address.Big())
	if err != nil {
		return nil, err
	}
	if len(value) == 0 {
		return nil, fmt.Errorf("no commitment found for address %s", address)
	}
	return value[0], nil
}
