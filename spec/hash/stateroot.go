package hash

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/spec/params"
)

// StateRoot computes the state root hash for the process parameters.
func StateRoot(processID, censusOrigin, pubKeyX, pubKeyY, packedBallotMode *big.Int) (*big.Int, error) {
	if processID == nil || censusOrigin == nil || pubKeyX == nil || pubKeyY == nil || packedBallotMode == nil {
		return nil, fmt.Errorf("state root: all inputs are required")
	}

	leafDomain := bigFromUint64(1)
	keyProcessID := bigFromUint64(params.StateKeyProcessID)
	keyBallotMode := bigFromUint64(params.StateKeyBallotMode)
	keyEncryptionKey := bigFromUint64(params.StateKeyEncryptionKey)
	keyResultsAdd := bigFromUint64(params.StateKeyResultsAdd)
	keyResultsSub := bigFromUint64(params.StateKeyResultsSub)
	keyCensusOrigin := bigFromUint64(params.StateKeyCensusOrigin)

	leafProcess, err := PoseidonHash(keyProcessID, processID, leafDomain)
	if err != nil {
		return nil, fmt.Errorf("state root: leaf process: %w", err)
	}
	leafBallot, err := PoseidonHash(keyBallotMode, packedBallotMode, leafDomain)
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

	zeroBallotHash := zeroBallotHashBig()
	leafResultsAdd, err := PoseidonHash(keyResultsAdd, zeroBallotHash, leafDomain)
	if err != nil {
		return nil, fmt.Errorf("state root: leaf results add: %w", err)
	}
	leafResultsSub, err := PoseidonHash(keyResultsSub, zeroBallotHash, leafDomain)
	if err != nil {
		return nil, fmt.Errorf("state root: leaf results sub: %w", err)
	}

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

func zeroBallotHashBig() *big.Int {
	value, ok := new(big.Int).SetString(ZeroBallotHashHex, 16)
	if !ok {
		panic("state root: invalid ZeroBallotHash hex")
	}
	return value
}

func bigFromUint64(value uint64) *big.Int {
	return new(big.Int).SetUint64(value)
}
