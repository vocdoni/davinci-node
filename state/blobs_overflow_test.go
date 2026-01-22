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
	resultsCells := 2 * coordsPerBallot
	cellsPerVote := 1 + 1 + coordsPerBallot
	maxVotes := (BlobTxFieldElementsPerBlob - resultsCells - 1) / cellsPerVote
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
	st.votes = votes

	_, err = st.BuildKZGCommitment()
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "blob overflow")
}
