package state

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/vocdoni/arbo"
	"github.com/vocdoni/davinci-node/types"
)

type arboTransitionTree interface {
	stateValueReader
	RootAsBigInt() (*big.Int, error)
	addBigInt(key *big.Int, values ...*big.Int) error
	updateBigInt(key *big.Int, values ...*big.Int) error
	generateGnarkVerifierProofBigInt(key *big.Int) (*arbo.GnarkVerifierProof, error)
}

type arboProofTree interface {
	generateGnarkVerifierProofBigInt(key *big.Int) (*arbo.GnarkVerifierProof, error)
}

type arboRootReader interface {
	RootAsBigInt() (*big.Int, error)
}

// ArboProof stores the proof in arbo native types
type ArboProof struct {
	// Key+Value hashed through Siblings path, should produce Root hash
	Root     *big.Int
	Siblings []*big.Int
	Key      *big.Int
	Value    *big.Int
}

// GenArboProof generates a ArboProof for the given key
func (s *State) GenArboProof(key types.StateKey) (*ArboProof, error) {
	return genArboProof(s, key)
}

// GenArboProof generates an ArboProof for the given key in the staged batch.
func (b *Batch) GenArboProof(key types.StateKey) (*ArboProof, error) {
	return genArboProof(b, key)
}

func genArboProof(tree arboProofTree, key types.StateKey) (*ArboProof, error) {
	proof, err := tree.generateGnarkVerifierProofBigInt(key.BigInt()) // TODO: refactor arbo to use uint64 instead
	if err != nil {
		return nil, err
	}
	return &ArboProof{
		Root:     proof.Root,
		Siblings: proof.Siblings,
		Key:      proof.Key,
		Value:    proof.Value,
	}, nil
}

// ArboProofsFromAddOrUpdate generates an ArboProof before adding (or updating) the given leaf,
// and another ArboProof after updating, and returns both.
func (s *State) ArboProofsFromAddOrUpdate(key types.StateKey, v []*big.Int) (*arbo.GnarkVerifierProof, *arbo.GnarkVerifierProof, error) {
	return arboProofsFromAddOrUpdate(s, key, v)
}

// ArboProofsFromAddOrUpdate generates the proof pair for a staged batch add or
// update.
func (b *Batch) ArboProofsFromAddOrUpdate(key types.StateKey, v []*big.Int) (*arbo.GnarkVerifierProof, *arbo.GnarkVerifierProof, error) {
	return arboProofsFromAddOrUpdate(b, key, v)
}

func arboProofsFromAddOrUpdate(tree arboTransitionTree, key types.StateKey, v []*big.Int) (*arbo.GnarkVerifierProof, *arbo.GnarkVerifierProof, error) {
	k := key.BigInt() // TODO: refactor arbo to use uint64 instead
	mpBefore, err := tree.generateGnarkVerifierProofBigInt(k)
	if err != nil {
		return nil, nil, err
	}
	if _, _, err := tree.getBigInt(k); err != nil {
		if !errors.Is(err, arbo.ErrKeyNotFound) {
			return nil, nil, fmt.Errorf("get key failed: %w", err)
		}
		if err := tree.addBigInt(k, v...); err != nil {
			return nil, nil, fmt.Errorf("add key failed: %w", err)
		}
	} else {
		if err := tree.updateBigInt(k, v...); err != nil {
			return nil, nil, fmt.Errorf("update key failed: %w", err)
		}
	}
	mpAfter, err := tree.generateGnarkVerifierProofBigInt(k)
	if err != nil {
		return nil, nil, err
	}
	return mpBefore, mpAfter, nil
}

// ArboTransition stores a pair of leaves and root hashes, and a single path common to both proofs
type ArboTransition struct {
	// NewKey + NewValue hashed through Siblings path, should produce NewRoot hash
	NewRoot  *big.Int
	Siblings []*big.Int
	NewKey   *big.Int
	NewValue *big.Int

	// OldKey + OldValue hashed through same Siblings should produce OldRoot hash
	OldRoot  *big.Int
	OldKey   *big.Int
	OldValue *big.Int
	IsOld0   int
	Fnc0     int
	Fnc1     int
}

// ArboTransitionFromArboProofPair generates a ArboTransition based on the pair of proofs passed
func ArboTransitionFromArboProofPair(before, after *arbo.GnarkVerifierProof) *ArboTransition {
	beforeIsInclusion := before.Fnc.Cmp(big.NewInt(0)) == 0
	afterIsInclusion := after.Fnc.Cmp(big.NewInt(0)) == 0
	//	Fnction
	//	fnc[0]  fnc[1]
	//	0       0       NOP
	//	0       1       UPDATE
	//	1       0       INSERT
	//	1       1       DELETE
	fnc0, fnc1 := 0, 0
	switch {
	case !beforeIsInclusion && !afterIsInclusion: // exclusion, exclusion = NOOP
		fnc0, fnc1 = 0, 0
	case beforeIsInclusion && afterIsInclusion: // inclusion, inclusion = UPDATE
		fnc0, fnc1 = 0, 1
	case !beforeIsInclusion && afterIsInclusion: // exclusion, inclusion = INSERT
		fnc0, fnc1 = 1, 0
	case beforeIsInclusion && !afterIsInclusion: // inclusion, exclusion = DELETE
		fnc0, fnc1 = 1, 1
	}
	oldKey := before.Key
	oldValue := before.Value
	if !beforeIsInclusion {
		oldKey = before.OldKey
		oldValue = before.OldValue
	}
	newKey := after.Key
	newValue := after.Value
	if !afterIsInclusion {
		newKey = after.OldKey
		newValue = after.OldValue
	}
	return &ArboTransition{
		Siblings: before.Siblings,
		OldRoot:  before.Root,
		OldKey:   oldKey,
		OldValue: oldValue,
		NewRoot:  after.Root,
		NewKey:   newKey,
		NewValue: newValue,
		IsOld0:   int(before.IsOld0.Int64()),
		Fnc0:     fnc0,
		Fnc1:     fnc1,
	}
}

// ArboTransitionFromAddOrUpdate adds or updates a key in the tree,
// and returns a ArboTransition.
func ArboTransitionFromAddOrUpdate(tree arboTransitionTree, k types.StateKey, v ...*big.Int) (*ArboTransition, error) {
	mpBefore, mpAfter, err := arboProofsFromAddOrUpdate(tree, k, v)
	if err != nil {
		return &ArboTransition{}, err
	}
	return ArboTransitionFromArboProofPair(mpBefore, mpAfter), nil
}

// ArboTransitionFromNoop returns a NOOP ArboTransition.
func ArboTransitionFromNoop(tree arboRootReader) (*ArboTransition, error) {
	root, err := tree.RootAsBigInt()
	if err != nil {
		return &ArboTransition{}, err
	}
	mp := &arbo.GnarkVerifierProof{
		Root:     root,
		Siblings: []*big.Int{},
		Key:      big.NewInt(0),
		Value:    big.NewInt(0),
		OldKey:   big.NewInt(0),
		OldValue: big.NewInt(0),
		IsOld0:   big.NewInt(0),
		Fnc:      big.NewInt(1), // exclusion
	}
	return ArboTransitionFromArboProofPair(mp, mp), nil
}
