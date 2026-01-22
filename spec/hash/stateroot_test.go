package hash

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/vocdoni/davinci-node/spec/params"
)

func TestZeroBallotHashConstant(t *testing.T) {
	inputs := make([]*big.Int, 0, 32)
	for range 8 {
		inputs = append(inputs, big.NewInt(0), big.NewInt(1), big.NewInt(0), big.NewInt(1))
	}
	got, err := PoseidonMultiHash(inputs)
	if err != nil {
		t.Fatalf("PoseidonHash error: %v", err)
	}
	want, ok := new(big.Int).SetString(ZeroBallotHashHex, 16)
	if !ok {
		t.Fatalf("invalid ZeroBallotHashHex")
	}
	if got.Cmp(want) != 0 {
		t.Fatalf("ZeroBallotHashHex mismatch: got %s want %s", got.String(), want.String())
	}
}

func TestLeafResultsConstants(t *testing.T) {
	zeroBallot, ok := new(big.Int).SetString(ZeroBallotHashHex, 16)
	if !ok {
		t.Fatalf("invalid ZeroBallotHashHex")
	}
	leafDomain := big.NewInt(1)

	wantAdd, ok := new(big.Int).SetString(LeafResultsAddHex, 16)
	if !ok {
		t.Fatalf("invalid LeafResultsAddHex")
	}
	wantSub, ok := new(big.Int).SetString(LeafResultsSubHex, 16)
	if !ok {
		t.Fatalf("invalid LeafResultsSubHex")
	}

	gotAdd, err := PoseidonHash(big.NewInt(int64(params.StateKeyResultsAdd)), zeroBallot, leafDomain)
	if err != nil {
		t.Fatalf("leaf results add hash error: %v", err)
	}
	gotSub, err := PoseidonHash(big.NewInt(int64(params.StateKeyResultsSub)), zeroBallot, leafDomain)
	if err != nil {
		t.Fatalf("leaf results sub hash error: %v", err)
	}

	if gotAdd.Cmp(wantAdd) != 0 {
		t.Fatalf("LeafResultsAddHex mismatch: got %s want %s", gotAdd.String(), wantAdd.String())
	}
	if gotSub.Cmp(wantSub) != 0 {
		t.Fatalf("LeafResultsSubHex mismatch: got %s want %s", gotSub.String(), wantSub.String())
	}
}

func TestStateRootMatchesManualConstruction(t *testing.T) {
	var b [32]byte
	rand.Read(b[:])
	processID := new(big.Int).SetBytes(b[:])
	censusOrigin := big.NewInt(6)
	pubKeyX := big.NewInt(123)
	pubKeyY := big.NewInt(456)
	packedBallotMode := big.NewInt(987654)

	zeroBallotHashBig, ok := new(big.Int).SetString(ZeroBallotHashHex, 16)
	if !ok {
		t.Fatalf("state root: invalid ZeroBallotHash hex")
	}

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

	leafProcess, err := PoseidonHash(keyProcessID,
		bigToFF(params.StateTransitionCurve.ScalarField(), processID), leafDomain)
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

	leafResultsAdd, err := PoseidonHash(keyResultsAdd, zeroBallotHashBig, leafDomain)
	if err != nil {
		t.Fatalf("leafResultsAdd error: %v", err)
	}
	leafResultsSub, err := PoseidonHash(keyResultsSub, zeroBallotHashBig, leafDomain)
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
