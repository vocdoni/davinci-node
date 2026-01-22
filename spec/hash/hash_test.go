package hash

import (
	"math/big"
	"testing"

	"github.com/vocdoni/davinci-node/spec/params"
)

func TestVoteIDMatchesTruncation(t *testing.T) {
	pid := big.NewInt(1)
	addr := big.NewInt(2)
	k := big.NewInt(3)

	h, err := PoseidonHash(pid, addr, k)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	got, err := VoteID(pid, addr, k)
	if err != nil {
		t.Fatalf("voteID: %v", err)
	}
	min := new(big.Int).SetUint64(params.VoteIDMin)
	expected := new(big.Int).Add(min, TruncateToLowerBits(h, params.VoteIDHashBits))
	if got.Cmp(expected) != 0 {
		t.Fatalf("mismatch: got %v want %v", got, expected)
	}
}
