package hash

import (
	"math/big"
	"testing"

	"github.com/vocdoni/davinci-node/spec/params"
)

func TestZeroBallotHashConstant(t *testing.T) {
	inputs := make([]*big.Int, 0, 32)
	for i := 0; i < 8; i++ {
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
