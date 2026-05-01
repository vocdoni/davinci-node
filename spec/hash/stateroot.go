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

// Results leaf is a constant derived from ZERO_BALLOT_HASH:
//
//	leafResults = H_3(KEY_RESULTS, ZERO_BALLOT_HASH, LEAF_DOMAIN)
const (
	LeafResultsHex = "1f72c52b6e5dedca4f99ecfa24f2776732431e8d544e14c6f78f5042727c4657"
)

// StateRoot computes the state root hash for the process parameters.
func StateRoot(processID, censusOrigin, pubKeyX, pubKeyY, ballotMode *big.Int) (*big.Int, error) {
	for _, bi := range []*big.Int{processID, censusOrigin, pubKeyX, pubKeyY, ballotMode} {
		if bi == nil {
			return nil, fmt.Errorf("state root: all inputs are required")
		}
		if bi.Sign() < 0 || bi.Cmp(params.StateTransitionCurve.ScalarField()) >= 0 {
			return nil, fmt.Errorf("state root: all inputs must be in field")
		}
	}

	leafDomain := bigFromUint64(1)
	keyProcessID := bigFromUint64(params.StateKeyProcessID)
	keyBallotMode := bigFromUint64(params.StateKeyBallotMode)
	keyEncryptionKey := bigFromUint64(params.StateKeyEncryptionKey)
	keyCensusOrigin := bigFromUint64(params.StateKeyCensusOrigin)

	leafProcess, err := PoseidonHash(keyProcessID, processID, leafDomain)
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

	leafResults := leafResultsBig()

	nodeA0, err := PoseidonHash(leafProcess, leafResults)
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
	root, err := PoseidonHash(nodeA, leafEncKey)
	if err != nil {
		return nil, fmt.Errorf("state root: root: %w", err)
	}
	return root, nil
}

func leafResultsBig() *big.Int {
	value, ok := new(big.Int).SetString(LeafResultsHex, 16)
	if !ok {
		panic("state root: invalid LeafResultsHex")
	}
	return value
}

func bigFromUint64(value uint64) *big.Int {
	return new(big.Int).SetUint64(value)
}
