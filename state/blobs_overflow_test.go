package state

import (
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

func TestBuildKZGCommitmentOverflow(t *testing.T) {
	c := qt.New(t)

	publicKey, _, err := elgamal.GenerateKey(Curve)
	c.Assert(err, qt.IsNil)

	st, err := New(memdb.New(), testutil.RandomProcessID())
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(st.Close(), qt.IsNil)
	}()

	err = st.Initialize(types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(),
		testutil.BallotModePacked(),
		types.EncryptionKeyFromPoint(publicKey),
	)
	c.Assert(err, qt.IsNil)

	coordsPerBallot := params.FieldsPerBallot * 4
	resultsCells := coordsPerBallot
	countCells := 1
	cellsPerVote := 1 + 1 + 1 + 1 + coordsPerBallot
	maxVotes := (BlobTxFieldElementsPerBlob - resultsCells - countCells) / cellsPerVote
	votes := make([]*Vote, 0, maxVotes+1)
	for i := 0; i < maxVotes+1; i++ {
		votes = append(votes, &Vote{
			Address:           big.NewInt(int64(i + 1)),
			VoteID:            testutil.RandomVoteID(),
			Ballot:            elgamal.NewBallot(Curve),
			ReencryptedBallot: elgamal.NewBallot(Curve),
			Weight:            big.NewInt(1),
		})
	}

	err = st.startBatch()
	c.Assert(err, qt.IsNil)
	st.rootHashBefore = big.NewInt(1)
	st.votes = votes

	_, err = st.computeBlobEvalData()
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "blob overflow")
}

func TestBlobEvalDataCachedAfterBatch(t *testing.T) {
	c := qt.New(t)

	publicKey, _, err := elgamal.GenerateKey(Curve)
	c.Assert(err, qt.IsNil)

	st, err := New(memdb.New(), testutil.RandomProcessID())
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(st.Close(), qt.IsNil)
	}()

	err = st.Initialize(
		types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(),
		testutil.BallotModePacked(),
		types.EncryptionKeyFromPoint(publicKey),
	)
	c.Assert(err, qt.IsNil)

	err = st.AddVotesBatch([]*Vote{
		{
			Address:           big.NewInt(1),
			BallotIndex:       types.CalculateBallotIndex(0),
			VoteID:            testutil.RandomVoteID(),
			Ballot:            elgamal.NewBallot(Curve),
			ReencryptedBallot: elgamal.NewBallot(Curve),
			Weight:            big.NewInt(1),
		},
	})
	c.Assert(err, qt.IsNil)

	firstBlobData, err := st.BlobEvalData()
	c.Assert(err, qt.IsNil)
	c.Assert(firstBlobData, qt.Not(qt.IsNil))

	secondBlobData, err := st.BlobEvalData()
	c.Assert(err, qt.IsNil)
	c.Assert(secondBlobData, qt.Equals, firstBlobData)
}
