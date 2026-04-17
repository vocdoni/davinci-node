package hash

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/vocdoni/arbo"
	"github.com/vocdoni/arbo/memdb"
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

func TestStateRootMatchesStateInitialization(t *testing.T) {
	processID, err := rand.Int(rand.Reader, params.StateTransitionCurve.ScalarField())
	if err != nil {
		t.Fatalf("rand.Int error: %v", err)
	}
	censusOrigin := big.NewInt(6)
	pubKeyX := big.NewInt(123)
	pubKeyY := big.NewInt(456)
	packedBallotMode := big.NewInt(987654)

	root, err := StateRoot(processID, censusOrigin, pubKeyX, pubKeyY, packedBallotMode)
	if err != nil {
		t.Fatalf("StateRoot error: %v", err)
	}

	tree, err := arbo.NewTree(arbo.Config{
		Database:     memdb.New(),
		MaxLevels:    params.StateTreeMaxLevels,
		HashFunction: arbo.HashFunctionMultiPoseidon,
	})
	if err != nil {
		t.Fatalf("new tree error: %v", err)
	}
	zeroBallot := make([]*big.Int, params.FieldsPerBallot*4)
	for i := range zeroBallot {
		if i%4 == 1 || i%4 == 3 {
			zeroBallot[i] = big.NewInt(1)
			continue
		}
		zeroBallot[i] = big.NewInt(0)
	}
	if err := tree.AddBigInt(big.NewInt(int64(params.StateKeyProcessID)), processID); err != nil {
		t.Fatalf("set process ID: %v", err)
	}
	if err := tree.AddBigInt(big.NewInt(int64(params.StateKeyBallotMode)), packedBallotMode); err != nil {
		t.Fatalf("set ballot mode: %v", err)
	}
	if err := tree.AddBigInt(big.NewInt(int64(params.StateKeyEncryptionKey)), pubKeyX, pubKeyY); err != nil {
		t.Fatalf("set encryption key: %v", err)
	}
	if err := tree.AddBigInt(big.NewInt(int64(params.StateKeyResults)), zeroBallot...); err != nil {
		t.Fatalf("set results: %v", err)
	}
	if err := tree.AddBigInt(big.NewInt(int64(params.StateKeyCensusOrigin)), censusOrigin); err != nil {
		t.Fatalf("set census origin: %v", err)
	}

	expectedRootBytes, err := tree.Root()
	if err != nil {
		t.Fatalf("tree root error: %v", err)
	}
	expectedRoot := arbo.BytesToBigInt(expectedRootBytes)

	if root.Cmp(expectedRoot) != 0 {
		t.Fatalf("state root mismatch: got %s want %s", root.String(), expectedRoot.String())
	}
}

func TestStateRootNilInputs(t *testing.T) {
	_, err := StateRoot(nil, big.NewInt(1), big.NewInt(1), big.NewInt(1), big.NewInt(1))
	if err == nil {
		t.Fatalf("expected error for nil processID")
	}
}

func TestResultsKeyConstant(t *testing.T) {
	if params.StateKeyResults != 0x04 {
		t.Fatalf("unexpected StateKeyResults: got %#x want %#x", params.StateKeyResults, 0x04)
	}
}
