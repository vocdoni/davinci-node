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
	// voteIDLeafValue is the value that VoteID leaves must have in the state merkle tree.
	voteIDLeafValue = big.NewInt(params.VoteIDLeafValue)

	// ErrNotFound is returned when a key is not found in the state
	ErrKeyNotFound = fmt.Errorf("not found")
)

const ballotTreeLeafBallotValueCount = params.FieldsPerBallot * 4

type stateValueReader interface {
	getBigInt(key *big.Int) (*big.Int, []*big.Int, error)
}

// TreeLeafValues returns the ballot leaf values in this order:
// ballot coordinates, address, weight.
func (v *Vote) TreeLeafValues() []*big.Int {
	values := append([]*big.Int{}, v.ReencryptedBallot.BigInts()...)
	address := v.Address
	if address == nil {
		address = big.NewInt(0)
	}
	weight := v.Weight
	if weight == nil {
		weight = big.NewInt(0)
	}
	values = append(values, address)
	return append(values, weight)
}

func ballotFromTreeLeafValues(values []*big.Int) (*elgamal.Ballot, error) {
	leaf, err := ballotLeafFromTreeLeafValues(values)
	if err != nil {
		return nil, err
	}
	return leaf.Ballot, nil
}

type BallotLeaf struct {
	Ballot  *elgamal.Ballot
	Address *big.Int
	Weight  *big.Int
}

func ballotLeafFromTreeLeafValues(values []*big.Int) (*BallotLeaf, error) {
	if len(values) < ballotTreeLeafBallotValueCount {
		return nil, fmt.Errorf("expected at least %d ballot values, got %d", ballotTreeLeafBallotValueCount, len(values))
	}
	ballot, err := elgamal.NewBallot(Curve).SetBigInts(values[:ballotTreeLeafBallotValueCount])
	if err != nil {
		return nil, err
	}
	address := big.NewInt(0)
	if len(values) > ballotTreeLeafBallotValueCount {
		address = values[ballotTreeLeafBallotValueCount]
	}
	weight := big.NewInt(0)
	if len(values) > ballotTreeLeafBallotValueCount+1 {
		weight = values[ballotTreeLeafBallotValueCount+1]
	}
	return &BallotLeaf{
		Ballot:  ballot,
		Address: address,
		Weight:  weight,
	}, nil
}

// Vote describes a vote with homomorphic ballot
type Vote struct {
	Address           *big.Int
	BallotIndex       types.BallotIndex
	VoteID            types.VoteID
	Ballot            *elgamal.Ballot
	OverwrittenBallot *elgamal.Ballot
	Weight            *big.Int
	ReencryptedBallot *elgamal.Ballot // Reencrypted ballot for the state transition circuit
}

// AddVote adds a vote to the state.
// If v.Address exists already in the tree, it counts as vote overwrite.
// Note that this method modifies passed v, sets v.OverwrittenBallot
func (b *Batch) addVote(v *Vote) error {
	if b.tx == nil {
		return fmt.Errorf("need to start batch first")
	}
	if len(b.votes) >= params.VotesPerBatch {
		return fmt.Errorf("too many votes for this batch")
	}
	// if address exists, it's a vote overwrite, need to count the overwritten
	// vote so it's later subtracted from circuit.Results.
	if oldLeaf, err := b.BallotLeaf(v.BallotIndex); err == nil {
		if oldLeaf.Address.Cmp(v.Address) != 0 || oldLeaf.Weight.Cmp(v.Weight) != 0 {
			return fmt.Errorf("stored ballot leaf metadata mismatch for ballot index %s", v.BallotIndex.String())
		}
		b.overwrittenSum.Add(b.overwrittenSum, oldLeaf.Ballot)
		b.overwrittenVotesCount++
		v.OverwrittenBallot = oldLeaf.Ballot
	} else {
		v.OverwrittenBallot = elgamal.NewBallot(Curve)
	}
	b.allBallotsSum.Add(b.allBallotsSum, v.ReencryptedBallot)
	b.votersCount++

	b.votes = append(b.votes, v)
	return nil
}

// EncryptedBallot returns the ballot associated with a address
func (s *State) EncryptedBallot(ballotIndex types.BallotIndex) (*elgamal.Ballot, error) {
	return encryptedBallotFromTree(s, ballotIndex)
}

func (tx *stateTreeTx) EncryptedBallot(ballotIndex types.BallotIndex) (*elgamal.Ballot, error) {
	return encryptedBallotFromTree(tx, ballotIndex)
}

func encryptedBallotFromTree(reader stateValueReader, ballotIndex types.BallotIndex) (*elgamal.Ballot, error) {
	_, value, err := reader.getBigInt(ballotIndex.BigInt())
	if err != nil {
		// Wrap arbo.ErrKeyNotFound to a specific error
		if errors.Is(err, arbo.ErrKeyNotFound) {
			return nil, ErrKeyNotFound
		}
		return nil, err
	}
	ballot, err := ballotFromTreeLeafValues(value)
	if err != nil {
		return nil, err
	}
	return ballot, nil
}

// BallotLeaf returns the stored ballot leaf associated with a ballot index.
func (s *State) BallotLeaf(ballotIndex types.BallotIndex) (*BallotLeaf, error) {
	return ballotLeafFromTree(s, ballotIndex)
}

// BallotLeaf returns the stored ballot leaf associated with a ballot index in
// the staged batch.
func (b *Batch) BallotLeaf(ballotIndex types.BallotIndex) (*BallotLeaf, error) {
	return ballotLeafFromTree(b, ballotIndex)
}

func ballotLeafFromTree(reader stateValueReader, ballotIndex types.BallotIndex) (*BallotLeaf, error) {
	_, value, err := reader.getBigInt(ballotIndex.BigInt())
	if err != nil {
		if errors.Is(err, arbo.ErrKeyNotFound) {
			return nil, ErrKeyNotFound
		}
		return nil, err
	}
	return ballotLeafFromTreeLeafValues(value)
}

// ContainsVoteID checks if the state contains a vote ID
func (s *State) ContainsVoteID(voteID types.VoteID) bool {
	if !voteID.Valid() {
		log.Errorf("voteID %d is invalid", voteID)
		return false
	}
	_, _, err := s.getBigInt(voteID.BigInt())
	return err == nil
}

// ContainsBallot checks if the state contains an address
func (s *State) ContainsBallot(ballotIndex types.BallotIndex) bool {
	_, _, err := s.getBigInt(ballotIndex.BigInt())
	return err == nil
}

// IndexContainsBallot checks if an address has voted in a given process. It opens
// the current process state and checks for the address. If found,
// it returns true, otherwise false. If there's an error opening the state or
// during the check, it returns the error.
func IndexContainsBallot(db db.Database, processID types.ProcessID, ballotIndex types.BallotIndex) (bool, error) {
	s, err := New(db, processID)
	if err != nil {
		return false, fmt.Errorf("could not open state: %v", err)
	}
	return s.ContainsBallot(ballotIndex), nil
}
