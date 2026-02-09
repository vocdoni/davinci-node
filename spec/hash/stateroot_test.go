package hash

import (
	"math/big"
	"testing"

	"github.com/vocdoni/davinci-node/spec/params"
)

func TestStateRootMatchesManualConstruction(t *testing.T) {
	processID := big.NewInt(42)
	censusOrigin := big.NewInt(6)
	pubKeyX := big.NewInt(123)
	pubKeyY := big.NewInt(456)
	packedBallotMode := big.NewInt(987654)

	root, err := StateRoot(processID, censusOrigin, pubKeyX, pubKeyY, packedBallotMode)
	if err != nil {
		t.Fatalf("StateRoot error: %v", err)
	}

	leafDomain := big.NewInt(1)
	keyProcessID := big.NewInt(int64(params.StateKeyProcessID))
	keyBallotMode := big.NewInt(int64(params.StateKeyBallotMode))
	keyEncryptionKey := big.NewInt(int64(params.StateKeyEncryptionKey))
	keyResultsAdd := big.NewInt(int64(params.StateKeyResultsAdd))
	keyResultsSub := big.NewInt(int64(params.StateKeyResultsSub))
	keyCensusOrigin := big.NewInt(int64(params.StateKeyCensusOrigin))

	leafProcess, err := PoseidonHash(keyProcessID, processID, leafDomain)
	if err != nil {
		t.Fatalf("leafProcess error: %v", err)
	}
	leafBallot, err := PoseidonHash(keyBallotMode, packedBallotMode, leafDomain)
	if err != nil {
		t.Fatalf("leafBallot error: %v", err)
	}
	encKey, err := PoseidonHash(pubKeyX, pubKeyY)
	if err != nil {
		t.Fatalf("encKey error: %v", err)
	}
	leafEncKey, err := PoseidonHash(keyEncryptionKey, encKey, leafDomain)
	if err != nil {
		t.Fatalf("leafEncKey error: %v", err)
	}
	leafCensus, err := PoseidonHash(keyCensusOrigin, censusOrigin, leafDomain)
	if err != nil {
		t.Fatalf("leafCensus error: %v", err)
	}

	leafResultsAdd, err := PoseidonHash(keyResultsAdd, zeroBallotHashBig(), leafDomain)
	if err != nil {
		t.Fatalf("leafResultsAdd error: %v", err)
	}
	leafResultsSub, err := PoseidonHash(keyResultsSub, zeroBallotHashBig(), leafDomain)
	if err != nil {
		t.Fatalf("leafResultsSub error: %v", err)
	}

	nodeA0, err := PoseidonHash(leafProcess, leafResultsAdd)
	if err != nil {
		t.Fatalf("nodeA0 error: %v", err)
	}
	nodeA1, err := PoseidonHash(leafBallot, leafCensus)
	if err != nil {
		t.Fatalf("nodeA1 error: %v", err)
	}
	nodeA, err := PoseidonHash(nodeA0, nodeA1)
	if err != nil {
		t.Fatalf("nodeA error: %v", err)
	}
	nodeB, err := PoseidonHash(leafResultsSub, leafEncKey)
	if err != nil {
		t.Fatalf("nodeB error: %v", err)
	}
	expected, err := PoseidonHash(nodeA, nodeB)
	if err != nil {
		t.Fatalf("expected root error: %v", err)
	}

	if root.Cmp(expected) != 0 {
		t.Fatalf("state root mismatch: got %s want %s", root.String(), expected.String())
	}
}

func TestStateRootNilInputs(t *testing.T) {
	_, err := StateRoot(nil, big.NewInt(1), big.NewInt(1), big.NewInt(1), big.NewInt(1))
	if err == nil {
		t.Fatalf("expected error for nil processID")
	}
}
