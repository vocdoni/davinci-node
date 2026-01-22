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

func TestStateKeyJSONRangeValidation(t *testing.T) {
	c := qt.New(t)

	belowVoteIDMinJSON, err := StateKey(params.VoteIDMin - 1).MarshalJSON()
	c.Assert(err, qt.IsNil)
	var voteID VoteID
	err = voteID.UnmarshalJSON(belowVoteIDMinJSON)
	c.Assert(err, qt.ErrorMatches, ".*VoteID.*out of range.*")

	atVoteIDMinJSON, err := StateKey(params.VoteIDMin).MarshalJSON()
	c.Assert(err, qt.IsNil)
	err = voteID.UnmarshalJSON(atVoteIDMinJSON)
	c.Assert(err, qt.IsNil)

	atVoteIDMaxJSON, err := StateKey(params.VoteIDMax).MarshalJSON()
	c.Assert(err, qt.IsNil)
	err = voteID.UnmarshalJSON(atVoteIDMaxJSON)
	c.Assert(err, qt.IsNil)

	// 0x01_0000000000000000 == 2^64, which is above uint64 max.
	// Hardcoded to exercise JSON ingestion of an out-of-range value.
	aboveVoteIDMaxJSON := []byte(`"0x010000000000000000"`)
	err = voteID.UnmarshalJSON(aboveVoteIDMaxJSON)
	c.Assert(err, qt.ErrorMatches, ".*VoteID.*too many bytes.*")

	belowBallotMinJSON, err := StateKey(params.BallotMin - 1).MarshalJSON()
	c.Assert(err, qt.IsNil)
	var ballotIndex BallotIndex
	err = ballotIndex.UnmarshalJSON(belowBallotMinJSON)
	c.Assert(err, qt.ErrorMatches, ".*BallotIndex.*out of range.*")

	atBallotMinJSON, err := StateKey(params.BallotMin).MarshalJSON()
	c.Assert(err, qt.IsNil)
	err = ballotIndex.UnmarshalJSON(atBallotMinJSON)
	c.Assert(err, qt.IsNil)

	atBallotMaxJSON, err := StateKey(params.BallotMax).MarshalJSON()
	c.Assert(err, qt.IsNil)
	err = ballotIndex.UnmarshalJSON(atBallotMaxJSON)
	c.Assert(err, qt.IsNil)

	aboveBallotMaxJSON, err := StateKey(params.BallotMax + 1).MarshalJSON()
	c.Assert(err, qt.IsNil)
	err = ballotIndex.UnmarshalJSON(aboveBallotMaxJSON)
	c.Assert(err, qt.ErrorMatches, ".*BallotIndex.*out of range.*")
}
