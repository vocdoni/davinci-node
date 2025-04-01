package state

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/vocdoni/arbo"
)

// ArboProof stores the proof in arbo native types
type ArboProof struct {
	// Key+Value hashed through Siblings path, should produce Root hash
	Root      *big.Int
	Siblings  []*big.Int
	Key       *big.Int
	Value     *big.Int
	Existence bool
}

// GenArboProof generates a ArboProof for the given key
func (o *State) GenArboProof(k *big.Int) (*ArboProof, error) {
	proof, err := o.tree.GenerateGnarkVerifierProofBigInt(k)
	if err != nil {
		return nil, err
	}
	return &ArboProof{
		Root:      proof.Root,
		Siblings:  proof.Siblings,
		Key:       proof.Key,
		Value:     proof.Value,
		Existence: proof.Fnc.Cmp(big.NewInt(1)) == 0,
	}, nil
}

// ArboProofsFromAddOrUpdate generates an ArboProof before adding (or updating) the given leaf,
// and another ArboProof after updating, and returns both.
func (o *State) ArboProofsFromAddOrUpdate(k *big.Int, v []*big.Int) (*ArboProof, *ArboProof, error) {
	mpBefore, err := o.GenArboProof(k)
	if err != nil {
		return nil, nil, err
	}
	if _, _, err := o.tree.GetBigInt(k); errors.Is(err, arbo.ErrKeyNotFound) {
		if err := o.tree.AddBigInt(k, v...); err != nil {
			return nil, nil, fmt.Errorf("add key failed: %w", err)
		}
	} else {
		if err := o.tree.UpdateBigInt(k, v...); err != nil {
			return nil, nil, fmt.Errorf("update key failed: %w", err)
		}
	}
	mpAfter, err := o.GenArboProof(k)
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
func ArboTransitionFromArboProofPair(before, after *ArboProof) *ArboTransition {
	//	Fnction
	//	fnc[0]  fnc[1]
	//	0       0       NOP
	//	0       1       UPDATE
	//	1       0       INSERT
	//	1       1       DELETE
	fnc0, fnc1 := 0, 0
	switch {
	case !before.Existence && !after.Existence: // exclusion, exclusion = NOOP
		fnc0, fnc1 = 0, 0
	case before.Existence && after.Existence: // inclusion, inclusion = UPDATE
		fnc0, fnc1 = 0, 1
	case !before.Existence && after.Existence: // exclusion, inclusion = INSERT
		fnc0, fnc1 = 1, 0
	case before.Existence && !after.Existence: // inclusion, exclusion = DELETE
		fnc0, fnc1 = 1, 1
	}

	isOld0 := 0
	if before.Key.Cmp(big.NewInt(0)) == 0 && before.Value.Cmp(big.NewInt(0)) == 0 {
		isOld0 = 1
	}

	return &ArboTransition{
		Siblings: before.Siblings,
		OldRoot:  before.Root,
		OldKey:   before.Key,
		OldValue: before.Value,
		NewRoot:  after.Root,
		NewKey:   after.Key,
		NewValue: after.Value,
		IsOld0:   isOld0,
		Fnc0:     fnc0,
		Fnc1:     fnc1,
	}
}

// ArboTransitionFromAddOrUpdate adds or updates a key in the tree,
// and returns a ArboTransition.
func ArboTransitionFromAddOrUpdate(o *State, k *big.Int, v ...*big.Int) (*ArboTransition, error) {
	mpBefore, mpAfter, err := o.ArboProofsFromAddOrUpdate(k, v)
	if err != nil {
		return &ArboTransition{}, err
	}
	return ArboTransitionFromArboProofPair(mpBefore, mpAfter), nil
}

// ArboTransitionFromNoop returns a NOOP ArboTransition.
func ArboTransitionFromNoop(o *State) (*ArboTransition, error) {
	root, err := o.Root()
	if err != nil {
		return &ArboTransition{}, err
	}
	mp := &ArboProof{
		Root:      arbo.BytesToBigInt(root),
		Siblings:  []*big.Int{},
		Key:       big.NewInt(0),
		Value:     big.NewInt(0),
		Existence: false,
	}
	return ArboTransitionFromArboProofPair(mp, mp), nil
}
