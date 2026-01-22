package state

import (
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/spec"
	"github.com/vocdoni/davinci-node/types"
)

func TestStateStoresPackedBallotMode(t *testing.T) {
	c := qt.New(t)

	processID := testutil.RandomProcessID()
	st, err := New(memdb.New(), processID)
	c.Assert(err, qt.IsNil)
	t.Cleanup(func() { _ = st.Close() })

	bm := spec.BallotMode{
		NumFields:      3,
		GroupSize:      2,
		UniqueValues:   true,
		CostFromWeight: false,
		CostExponent:   1,
		MaxValue:       10,
		MinValue:       0,
		MaxValueSum:    20,
		MinValueSum:    0,
	}
	packed, err := bm.Pack()
	c.Assert(err, qt.IsNil)

	publicKey, _, err := elgamal.GenerateKey(Curve)
	c.Assert(err, qt.IsNil)
	encryptionKeyCircuit := circuits.EncryptionKeyFromECCPoint(publicKey)

	err = st.Initialize(types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(), packed, encryptionKeyCircuit)
	c.Assert(err, qt.IsNil)

	_, values, err := st.tree.GetBigInt(KeyBallotMode.BigInt())
	c.Assert(err, qt.IsNil)
	c.Assert(values, qt.HasLen, 1)
	c.Assert(values[0].Cmp(packed), qt.Equals, 0)

	// Ensure we stored the exact packed value (not just equivalent bytes).
	c.Assert(new(big.Int).Set(values[0]).Cmp(packed), qt.Equals, 0)
}
