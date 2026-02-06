package types

import (
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/spec/params"
)

func TestStateKeys(t *testing.T) {
	b := BallotIndex(42)
	qt.Assert(t, b.BigInt().Cmp(big.NewInt(42)), qt.Equals, 0)
	b.Bytes()
	b.IsInField(b.BigInt())
	qt.Assert(t, b.Valid(), qt.IsTrue)
	qt.Assert(t, b.Uint64(), qt.Equals, uint64(42))
	qt.Assert(t, b.String(), qt.Equals, "0x000000000000002a")

	json, err := b.MarshalJSON()
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, b.UnmarshalJSON(json), qt.IsNil)

	_, err = HexStringToBallotIndex("0x00")
	qt.Assert(t, err, qt.ErrorMatches, ".*out of range.*")
	_, err = HexStringToBallotIndex("0x10")
	qt.Assert(t, err, qt.IsNil)

	_, err = BigIntToBallotIndex(new(big.Int).SetUint64(params.BallotMin - 1))
	qt.Assert(t, err, qt.ErrorMatches, ".*out of range.*")
	_, err = BigIntToBallotIndex(new(big.Int).SetUint64(params.BallotMax + 1))
	qt.Assert(t, err, qt.ErrorMatches, ".*out of range.*")

	_, err = BigIntToBallotIndex(new(big.Int).SetUint64(params.BallotMin))
	qt.Assert(t, err, qt.IsNil)
	_, err = BigIntToBallotIndex(new(big.Int).SetUint64(params.BallotMax))
	qt.Assert(t, err, qt.IsNil)
}
