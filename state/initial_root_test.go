package state

import (
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/spec"
	"github.com/vocdoni/davinci-node/types"
)

func TestInitializeRootMatchesSpecHash(t *testing.T) {
	c := qt.New(t)

	processID := testutil.RandomProcessID()
	ballotMode := testutil.BallotMode()
	packedBallotMode, err := ballotMode.Pack()
	c.Assert(err, qt.IsNil)

	publicKey, _, err := elgamal.GenerateKey(Curve)
	c.Assert(err, qt.IsNil)

	censusOrigin := types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt()

	st, err := New(memdb.New(), processID)
	c.Assert(err, qt.IsNil)
	t.Cleanup(func() { _ = st.Close() })

	err = st.Initialize(censusOrigin, packedBallotMode, types.EncryptionKeyFromPoint(publicKey))
	c.Assert(err, qt.IsNil)

	got, err := st.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	want, err := spec.StateRoot(
		processID.MathBigInt(),
		censusOrigin,
		publicKey.BigInts()[0],
		publicKey.BigInts()[1],
		packedBallotMode,
	)
	c.Assert(err, qt.IsNil)

	c.Assert(got.Cmp(want), qt.Equals, 0)
}
