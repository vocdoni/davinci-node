package merkleproof

import (
	"fmt"
	"math/big"

	"github.com/consensys/gnark/frontend"
	"github.com/vocdoni/arbo"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/util"
	"github.com/vocdoni/gnark-crypto-primitives/tree/smt"
	"github.com/vocdoni/gnark-crypto-primitives/utils"
)

// MerkleProof stores the leaf, the path, and the root hash.
type MerkleProof struct {
	// Key + Value hashed through Siblings path, should produce Root hash
	Root     frontend.Variable
	Siblings [params.StateTreeMaxLevels]frontend.Variable
	Key      frontend.Variable
	LeafHash frontend.Variable
}

// MerkleProofFromArboProof converts an ArboProof into a MerkleProof
func MerkleProofFromArboProof(p *state.ArboProof) (MerkleProof, error) {
	bKey := state.EncodeKey(p.Key)
	bValue := state.HashFn.SafeBigInt(p.Value)
	leafHash, err := state.HashFn.Hash(bKey, bValue, []byte{1})
	if err != nil {
		return MerkleProof{}, fmt.Errorf("failed to hash leaf: %w", err)
	}
	return MerkleProof{
		Root:     p.Root,
		Siblings: padStateSiblings(p.Siblings),
		Key:      p.Key,
		LeafHash: arbo.BytesToBigInt(leafHash),
	}, nil
}

// Verify uses smt.Verifier to verify that:
//   - mp.Root matches passed root
//   - mp.LeafHash at position Key belongs to mp.Root
func (mp *MerkleProof) Verify(api frontend.API, hFn utils.Hasher, root frontend.Variable) {
	api.AssertIsEqual(root, mp.Root)
	smt.VerifierWithLeafHash(api, hFn,
		1,
		mp.Root,
		mp.Siblings[:],
		mp.Key,
		mp.LeafHash,
		0,
		mp.Key,
		mp.LeafHash,
		0, // inclusion
	)
}

// VerifyLeafHash asserts that smt.Hash1(mp.Key, values...) matches mp.LeafHash.
// It encodes the values using the provided hash function hFn in the same way
// that arbo does when it works with big.Ints. It returns an error if the
// encoding fails.
func (mp *MerkleProof) VerifyLeafHash(api frontend.API, hFn utils.Hasher, values ...frontend.Variable) error {
	encodedValue, err := encodeLeafValue(api, hFn, values...)
	if err != nil {
		return fmt.Errorf("encode the values of the leaf: %w", err)
	}
	api.AssertIsEqual(mp.LeafHash, smt.Hash1(api, hFn, mp.Key, encodedValue))
	return nil
}

func (mp *MerkleProof) String() string {
	return fmt.Sprint(mp.Key, "=", util.PrettyHex(mp.LeafHash), " -> ", util.PrettyHex(mp.Root))
}

// MerkleTransition stores a pair of leaves and root hashes, and a single path common to both proofs
type MerkleTransition struct {
	// NewKey + NewValue hashed through Siblings path, should produce NewRoot hash
	NewRoot     frontend.Variable
	Siblings    [params.StateTreeMaxLevels]frontend.Variable
	NewKey      frontend.Variable
	NewLeafHash frontend.Variable
	// OldKey + OldValue hashed through same Siblings should produce OldRoot hash
	OldRoot     frontend.Variable
	OldKey      frontend.Variable
	OldLeafHash frontend.Variable
	IsOld0      frontend.Variable
	Fnc0        frontend.Variable
	Fnc1        frontend.Variable
}

// MerkleTransitionFromArboTransition converts an ArboTransition into a
// MerkleTransition. It calculates the old and new leaf hashes using the
// EncodeKey and Hash functions from the state package. The leaf hashes are
// transformed using arbo helper functions to ensure they are in the correct
// endianess. The function also pads the siblings to the maximum number of
// levels in the census tree. It returns an error if the hashing fails.
func MerkleTransitionFromArboTransition(at *state.ArboTransition) (MerkleTransition, error) {
	bOldKey := state.EncodeKey(at.OldKey)
	bOldValue := state.HashFn.SafeBigInt(at.OldValue)
	oldLeafHash, err := state.HashFn.Hash(bOldKey, bOldValue, []byte{1})
	if err != nil {
		return MerkleTransition{}, err
	}
	bNewKey := state.EncodeKey(at.NewKey)
	bNewValue := state.HashFn.SafeBigInt(at.NewValue)
	newLeafHash, err := state.HashFn.Hash(bNewKey, bNewValue, []byte{1})
	if err != nil {
		return MerkleTransition{}, err
	}
	return MerkleTransition{
		NewRoot:     at.NewRoot,
		Siblings:    padStateSiblings(at.Siblings),
		NewKey:      at.NewKey,
		NewLeafHash: arbo.BytesToBigInt(newLeafHash),
		OldRoot:     at.OldRoot,
		OldKey:      at.OldKey,
		OldLeafHash: arbo.BytesToBigInt(oldLeafHash),
		IsOld0:      at.IsOld0,
		Fnc0:        at.Fnc0,
		Fnc1:        at.Fnc1,
	}, nil
}

// Verify uses smt.Processor to verify that:
//   - mp.OldRoot matches passed oldRoot
//   - OldKey + OldValue belong to OldRoot
//   - NewKey + NewValue belong to NewRoot
//   - no other changes were introduced between OldRoot -> NewRoot
//
// and returns mp.NewRoot
func (mp *MerkleTransition) Verify(api frontend.API, hFn utils.Hasher, oldRoot frontend.Variable) frontend.Variable {
	api.AssertIsEqual(oldRoot, mp.OldRoot)
	root := smt.ProcessorWithLeafHash(api, hFn,
		mp.OldRoot,
		mp.Siblings[:],
		mp.OldKey,
		mp.OldLeafHash,
		mp.IsOld0,
		mp.NewKey,
		mp.NewLeafHash,
		mp.Fnc0,
		mp.Fnc1,
	)
	api.AssertIsEqual(root, mp.NewRoot)
	return mp.NewRoot
}

// VerifyOldLeafHash asserts that smt.Hash1(mp.OldKey, values...) matches mp.OldLeafHash,
// only when the MerkleTransition is not a NOOP
func (mp *MerkleTransition) VerifyOldLeafHash(api frontend.API, hFn utils.Hasher, values ...frontend.Variable) error {
	return verifyLeafHash(api, hFn, mp.OldKey, mp.OldLeafHash, mp.IsNoop(api), values...)
}

// VerifyNewLeafHash asserts that smt.Hash1(mp.NewKey, values...) matches mp.NewLeafHash,
// only when the MerkleTransition is not a NOOP
func (mp *MerkleTransition) VerifyNewLeafHash(api frontend.API, hFn utils.Hasher, values ...frontend.Variable) error {
	return verifyLeafHash(api, hFn, mp.NewKey, mp.NewLeafHash, mp.IsNoop(api), values...)
}

// VerifyOverwrittenBallot asserts that smt.Hash1(mp.OldKey, values...) matches mp.OldLeafHash,
// only when the MerkleTransition is an UPDATE
func (mp *MerkleTransition) VerifyOverwrittenBallot(api frontend.API, hFn utils.Hasher, values ...frontend.Variable) error {
	return verifyLeafHash(api, hFn, mp.OldKey, mp.OldLeafHash, api.IsZero(mp.IsUpdate(api)), values...)
}

func verifyLeafHash(
	api frontend.API,
	hFn utils.Hasher,
	key, leafHash, skip frontend.Variable,
	values ...frontend.Variable,
) error {
	encodedValue, err := encodeLeafValue(api, hFn, values...)
	if err != nil {
		return fmt.Errorf("encode the values of the leaf: %w", err)
	}
	// used to skip the assert, for example when MerkleTransition is NOOP or not an UPDATE
	api.AssertIsEqual(leafHash, api.Select(skip, leafHash, smt.Hash1(api, hFn, key, encodedValue)))
	return nil
}

// encodeLeafValue mirrors arbo bigIntsToLeaf behavior:
// single-value leaves use the value directly, while multi-value leaves hash
// values first and use that hash as the leaf value.
func encodeLeafValue(api frontend.API, hFn utils.Hasher, values ...frontend.Variable) (frontend.Variable, error) {
	if len(values) == 1 {
		return values[0], nil
	}
	return hFn(api, values...)
}

// VerifyNewKey asserts that value matches mp.NewKey,
// only when the MerkleTransition is not a NOOP
func (mp *MerkleTransition) VerifyNewKey(api frontend.API, value frontend.Variable) {
	verifyLeafKey(api, mp.NewKey, mp.IsNoop(api), value)
}

func verifyLeafKey(api frontend.API, key, skip frontend.Variable, value frontend.Variable) {
	// used to skip the assert, for example when MerkleTransition is NOOP or not an UPDATE
	api.AssertIsEqual(key, api.Select(skip, key, value))
}

func (mp *MerkleTransition) String() string {
	return fmt.Sprint(util.PrettyHex(mp.OldRoot), " -> ", util.PrettyHex(mp.NewRoot), " | ",
		mp.OldKey, "=", util.PrettyHex(mp.OldLeafHash), " -> ", mp.NewKey, "=", util.PrettyHex(mp.NewLeafHash))
}

// IsUpdate returns true when mp.Fnc0 == 0 && mp.Fnc1 == 1
func (mp *MerkleTransition) IsUpdate(api frontend.API) frontend.Variable {
	fnc0IsZero := api.IsZero(mp.Fnc0)
	fnc1IsOne := api.Sub(1, api.IsZero(mp.Fnc1))
	return api.And(fnc0IsZero, fnc1IsOne)
}

// IsInsert returns true when mp.Fnc0 == 1 && mp.Fnc1 == 0
func (mp *MerkleTransition) IsInsert(api frontend.API) frontend.Variable {
	fnc0IsOne := api.Sub(1, api.IsZero(mp.Fnc0))
	fnc1IsZero := api.IsZero(mp.Fnc1)
	return api.And(fnc1IsZero, fnc0IsOne)
}

// IsInsertOrUpdate returns true when IsInsert or IsUpdate is true
func (mp *MerkleTransition) IsInsertOrUpdate(api frontend.API) frontend.Variable {
	return api.Or(mp.IsInsert(api), mp.IsUpdate(api))
}

// IsNoop returns true when mp.Fnc0 == 0 && mp.Fnc1 == 0
func (mp *MerkleTransition) IsNoop(api frontend.API) frontend.Variable {
	return api.And(api.IsZero(mp.Fnc0), api.IsZero(mp.Fnc1))
}

// padStateSiblings pads the unpacked siblings to the maximum number of levels
// in the census tree, filling with 0s if needed. It returns a fixed-size array
// of the maximum number of levels of frontend.Variable.
func padStateSiblings(unpackedSiblings []*big.Int) [params.StateTreeMaxLevels]frontend.Variable {
	paddedSiblings := [params.StateTreeMaxLevels]frontend.Variable{}
	for i, v := range circuits.BigIntArrayToN(unpackedSiblings, params.StateTreeMaxLevels) {
		paddedSiblings[i] = v
	}
	return paddedSiblings
}
