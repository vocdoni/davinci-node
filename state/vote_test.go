package state

import (
	"math/big"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/types"
)

func TestTreeLeafValuesIncludesAddressAndWeight(t *testing.T) {
	c := qt.New(t)

	vote := &Vote{
		Address:           big.NewInt(3),
		ReencryptedBallot: elgamal.NewBallot(Curve),
		Weight:            big.NewInt(17),
	}

	values := vote.TreeLeafValues()
	c.Assert(values, qt.HasLen, ballotTreeLeafBallotValueCount+2)
	c.Assert(values[len(values)-2].Cmp(vote.Address), qt.Equals, 0)
	c.Assert(values[len(values)-1].Cmp(vote.Weight), qt.Equals, 0)
}

func TestEncryptedBallotReadsTreeLeafMetadata(t *testing.T) {
	c := qt.New(t)

	publicKey, _, err := elgamal.GenerateKey(Curve)
	c.Assert(err, qt.IsNil)

	st, err := New(memdb.New(), testutil.RandomProcessID())
	c.Assert(err, qt.IsNil)
	err = st.Initialize(
		types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(),
		testutil.BallotModePacked(),
		types.EncryptionKeyFromPoint(publicKey),
	)
	c.Assert(err, qt.IsNil)

	vote := &Vote{
		Address:           big.NewInt(1),
		BallotIndex:       types.CalculateBallotIndex(0),
		VoteID:            testutil.RandomVoteID(),
		Ballot:            elgamal.NewBallot(Curve),
		ReencryptedBallot: elgamal.NewBallot(Curve),
		Weight:            big.NewInt(17),
	}

	err = st.tree.AddBigInt(vote.BallotIndex.BigInt(), vote.TreeLeafValues()...)
	c.Assert(err, qt.IsNil)

	_, values, err := st.tree.GetBigInt(vote.BallotIndex.BigInt())
	c.Assert(err, qt.IsNil)
	c.Assert(values, qt.HasLen, ballotTreeLeafBallotValueCount+2)
	c.Assert(values[len(values)-2].Cmp(vote.Address), qt.Equals, 0)
	c.Assert(values[len(values)-1].Cmp(vote.Weight), qt.Equals, 0)

	ballot, err := st.EncryptedBallot(vote.BallotIndex)
	c.Assert(err, qt.IsNil)
	for i, expected := range vote.ReencryptedBallot.BigInts() {
		c.Assert(ballot.BigInts()[i].Cmp(expected), qt.Equals, 0)
	}
}

func TestAddVoteRejectsOverwriteWithMismatchedStoredMetadata(t *testing.T) {
	c := qt.New(t)

	publicKey, _, err := elgamal.GenerateKey(Curve)
	c.Assert(err, qt.IsNil)

	st, err := New(memdb.New(), testutil.RandomProcessID())
	c.Assert(err, qt.IsNil)
	err = st.Initialize(
		types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(),
		testutil.BallotModePacked(),
		types.EncryptionKeyFromPoint(publicKey),
	)
	c.Assert(err, qt.IsNil)

	vote := &Vote{
		Address:           big.NewInt(1),
		BallotIndex:       types.CalculateBallotIndex(0),
		VoteID:            testutil.RandomVoteID(),
		Ballot:            elgamal.NewBallot(Curve),
		ReencryptedBallot: elgamal.NewBallot(Curve),
		Weight:            big.NewInt(17),
	}

	err = st.tree.AddBigInt(vote.BallotIndex.BigInt(), vote.TreeLeafValues()...)
	c.Assert(err, qt.IsNil)

	vote.Address = big.NewInt(2)
	err = st.AddVotesBatch([]*Vote{vote})
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(strings.Contains(err.Error(), "stored ballot leaf metadata mismatch"), qt.IsTrue)
}
