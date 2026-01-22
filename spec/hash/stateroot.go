package hash

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/spec/params"
)

// ZeroBallotHashHex (a.k.a ZERO_BALLOT_HASH) is the Poseidon hash of 8 fields where each
// field is the 4-tuple (0, 1, 0, 1) (babyjubjub identity points):
//
//	zeroBallotValues = [
//	 0,1,0,1,  0,1,0,1,  0,1,0,1,  0,1,0,1,
//	 0,1,0,1,  0,1,0,1,  0,1,0,1,  0,1,0,1
//	]
const ZeroBallotHashHex = "2c66ee3d8ff0f86c2251e885d4c207e5162c05d0b458c773106cd5579c58bf36"

// Results leaves are constants derived from ZERO_BALLOT_HASH:
//
//	leafResultsAdd = H_3(KEY_RESULTS_ADD, ZERO_BALLOT_HASH, LEAF_DOMAIN)
//	leafResultsSub = H_3(KEY_RESULTS_SUB, ZERO_BALLOT_HASH, LEAF_DOMAIN)
const (
	LeafResultsAddHex = "1f72c52b6e5dedca4f99ecfa24f2776732431e8d544e14c6f78f5042727c4657"
	LeafResultsSubHex = "2b853c511fba705a6030f80ce83d6ee8cbf4a1273724368728c11682eae4c51a"
)

// StateRoot computes the state root hash for the process parameters.
func StateRoot(processID, censusOrigin, pubKeyX, pubKeyY, ballotMode *big.Int) (*big.Int, error) {
	if processID == nil || censusOrigin == nil || pubKeyX == nil || pubKeyY == nil || ballotMode == nil {
		return nil, fmt.Errorf("state root: all inputs are required")
	}

	leafDomain := bigFromUint64(1)
	keyProcessID := bigFromUint64(params.StateKeyProcessID)
	keyBallotMode := bigFromUint64(params.StateKeyBallotMode)
	keyEncryptionKey := bigFromUint64(params.StateKeyEncryptionKey)
	keyCensusOrigin := bigFromUint64(params.StateKeyCensusOrigin)

	leafProcess, err := PoseidonHash(keyProcessID,
		bigToFF(params.StateTransitionCurve.ScalarField(), processID), leafDomain)
	if err != nil {
		return nil, fmt.Errorf("state root: leaf process: %w", err)
	}
	leafBallot, err := PoseidonHash(keyBallotMode, ballotMode, leafDomain)
	if err != nil {
		return nil, fmt.Errorf("state root: leaf ballot mode: %w", err)
	}
	encKey, err := PoseidonHash(pubKeyX, pubKeyY)
	if err != nil {
		return nil, fmt.Errorf("state root: encryption key hash: %w", err)
	}
	leafEncKey, err := PoseidonHash(keyEncryptionKey, encKey, leafDomain)
	if err != nil {
		return nil, fmt.Errorf("state root: leaf encryption key: %w", err)
	}
	leafCensus, err := PoseidonHash(keyCensusOrigin, censusOrigin, leafDomain)
	if err != nil {
		return nil, fmt.Errorf("state root: leaf census origin: %w", err)
	}

	leafResultsAdd := leafResultsAddBig()
	leafResultsSub := leafResultsSubBig()

	nodeA0, err := PoseidonHash(leafProcess, leafResultsAdd)
	if err != nil {
		return nil, fmt.Errorf("state root: nodeA0: %w", err)
	}
	nodeA1, err := PoseidonHash(leafBallot, leafCensus)
	if err != nil {
		return nil, fmt.Errorf("state root: nodeA1: %w", err)
	}
	nodeA, err := PoseidonHash(nodeA0, nodeA1)
	if err != nil {
		return nil, fmt.Errorf("state root: nodeA: %w", err)
	}
	nodeB, err := PoseidonHash(leafResultsSub, leafEncKey)
	if err != nil {
		return nil, fmt.Errorf("state root: nodeB: %w", err)
	}
	root, err := PoseidonHash(nodeA, nodeB)
	if err != nil {
		return nil, fmt.Errorf("state root: root: %w", err)
	}
	return root, nil
}

func leafResultsAddBig() *big.Int {
	value, ok := new(big.Int).SetString(LeafResultsAddHex, 16)
	if !ok {
		panic("state root: invalid LeafResultsAddHex")
	}
	return value
}

func leafResultsSubBig() *big.Int {
	value, ok := new(big.Int).SetString(LeafResultsSubHex, 16)
	if !ok {
		panic("state root: invalid LeafResultsSubHex")
	}
	return value
}

func bigFromUint64(value uint64) *big.Int {
	return new(big.Int).SetUint64(value)
}
