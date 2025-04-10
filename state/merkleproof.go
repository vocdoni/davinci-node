package state

import (
	"errors"
	"fmt"
	"log"
	"math/big"

	"github.com/vocdoni/arbo"
	"github.com/vocdoni/vocdoni-z-sandbox/util"
)

// ArboProof stores the proof in arbo native types
type ArboProof struct {
	// Key+Value hashed through Siblings path, should produce Root hash
	Root     *big.Int
	Siblings []*big.Int
	Key      *big.Int
	Value    *big.Int
}

// GenArboProof generates a ArboProof for the given key
func (o *State) GenArboProof(k *big.Int) (*ArboProof, error) {
	fmt.Println("\nGenArboProof", k)
	proof, err := o.tree.GenerateGnarkVerifierProofBigInt(k)
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
func (o *State) ArboProofsFromAddOrUpdate(k *big.Int, v []*big.Int) (*arbo.GnarkVerifierProof, *arbo.GnarkVerifierProof, error) {
	fmt.Println("\nArboProofsFromAddOrUpdate for key", util.PrettyHex(k), len(v))
	mpBefore, err := o.tree.GenerateGnarkVerifierProofBigInt(k)
	if err != nil {
		return nil, nil, err
	}
	fmt.Println("ArboProofsFromAddOrUpdate mpBefore",
		"Key", util.PrettyHex(mpBefore.Key),
		"Value", util.PrettyHex(mpBefore.Value),
		"OldKey", util.PrettyHex(mpBefore.OldKey),
		"OldValue", util.PrettyHex(mpBefore.OldValue),
		"Root", util.PrettyHex(mpBefore.Root),
	)
	bv, _ := arbo.EncodeBigIntValues(HashFunc, v...)
	if _, oldValue, err := o.tree.GetBigInt(k); errors.Is(err, arbo.ErrKeyNotFound) {
		log.Println("adding key", k, "->", arbo.BytesToBigInt(bv))
		if err := o.tree.AddBigInt(k, v...); err != nil {
			return nil, nil, fmt.Errorf("add key failed: %w", err)
		}
	} else {
		oldbv, _ := arbo.EncodeBigIntValues(HashFunc, oldValue...)
		log.Println("updating key", k, "->", arbo.BytesToBigInt(bv), "old value", arbo.BytesToBigInt(oldbv))
		if err := o.tree.UpdateBigInt(k, v...); err != nil {
			return nil, nil, fmt.Errorf("update key failed: %w", err)
		}
	}
	mpAfter, err := o.tree.GenerateGnarkVerifierProofBigInt(k)
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

	fmt.Println("ArboTransitionFromArboProofPair",
		"before.Key", util.PrettyHex(oldKey),
		"before.Value", util.PrettyHex(oldValue),
		"after.Key", util.PrettyHex(newKey),
		"after.Value", util.PrettyHex(newValue),
		"isOld0", util.PrettyHex(isOld0),
		"Fnc0", util.PrettyHex(fnc0),
		"Fnc1", util.PrettyHex(fnc1),
	)

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
	mp := &arbo.GnarkVerifierProof{
		Root:     arbo.BytesToBigInt(root),
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
