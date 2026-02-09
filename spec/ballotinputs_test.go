package spec

import (
	"math/big"
	"testing"

	"github.com/vocdoni/davinci-node/spec/hash"
)

func TestBallotInputsHashRTE(t *testing.T) {
	bm := BallotMode{
		NumFields:      3,
		GroupSize:      2,
		UniqueValues:   true,
		CostFromWeight: false,
		CostExponent:   2,
		MaxValue:       10,
		MinValue:       0,
		MaxValueSum:    20,
		MinValueSum:    0,
	}

	processID := big.NewInt(123)
	address := big.NewInt(456)
	voteID := big.NewInt(789)
	weight := big.NewInt(5)
	encKeyX := big.NewInt(111)
	encKeyY := big.NewInt(222)

	ballot := make([]*big.Int, 0, 8)
	for i := 0; i < 8; i++ {
		ballot = append(ballot, big.NewInt(int64(i+1)))
	}

	got, err := BallotInputsHashRTE(processID, bm, encKeyX, encKeyY, address, voteID, ballot, weight)
	if err != nil {
		t.Fatalf("BallotInputsHashRTE error: %v", err)
	}

	packed, err := bm.Pack()
	if err != nil {
		t.Fatalf("pack ballot mode: %v", err)
	}

	inputs := []*big.Int{
		processID,
		packed,
		encKeyX,
		encKeyY,
		address,
		voteID,
	}
	inputs = append(inputs, ballot...)
	inputs = append(inputs, weight)

	want, err := hash.PoseidonMultiHash(inputs)
	if err != nil {
		t.Fatalf("expected hash error: %v", err)
	}

	if got.Cmp(want) != 0 {
		t.Fatalf("hash mismatch: got %s want %s", got.String(), want.String())
	}
}
