package eddsa

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

func TestGenerateVerifyProof(t *testing.T) {
	c := qt.New(t)

	processID := testutil.RandomProcessID()
	userAddress := testutil.RandomAddress()
	userWeight := types.NewInt(testutil.Weight)

	csp, err := New(DefaultHashFn)
	c.Assert(err, qt.IsNil)

	t.Run("invalid inputs", func(t *testing.T) {
		_, err = csp.GenerateProof(types.ProcessID{}, userAddress, userWeight)
		c.Assert(err, qt.IsNotNil)

		_, err = csp.GenerateProof(processID, common.Address{}, userWeight)
		c.Assert(err, qt.IsNotNil)

		_, err = csp.GenerateProof(processID, userAddress, nil)
		c.Assert(err, qt.IsNotNil)
	})

	t.Run("valid proof generation and verification", func(t *testing.T) {
		proof, err := csp.GenerateProof(processID, userAddress, userWeight)
		c.Assert(err, qt.IsNil)
		c.Assert(proof, qt.IsNotNil)

		err = csp.VerifyProof(proof)
		c.Assert(err, qt.IsNil)
	})

	t.Run("invalid proof pubkey", func(t *testing.T) {
		proof, err := csp.GenerateProof(processID, userAddress, userWeight)
		c.Assert(err, qt.IsNil)
		c.Assert(proof, qt.IsNotNil)

		proof.PublicKey = util.RandomBytes(20)
		err = csp.VerifyProof(proof)
		c.Assert(err, qt.IsNotNil)
	})

	t.Run("invalid proof address", func(t *testing.T) {
		proof, err := csp.GenerateProof(processID, userAddress, userWeight)
		c.Assert(err, qt.IsNil)
		c.Assert(proof, qt.IsNotNil)

		proof.Address = testutil.RandomAddress().Bytes()
		err = csp.VerifyProof(proof)
		c.Assert(err, qt.IsNotNil)
	})

	t.Run("invalid proof signature", func(t *testing.T) {
		proof, err := csp.GenerateProof(processID, userAddress, userWeight)
		c.Assert(err, qt.IsNil)
		c.Assert(proof, qt.IsNotNil)

		proof.Signature = util.RandomBytes(20)
		err = csp.VerifyProof(proof)
		c.Assert(err, qt.IsNotNil)
	})
}

func TestCensusRootLengthAndValue(t *testing.T) {
	c := qt.New(t)

	for range 1000 {
		csp, err := New(DefaultHashFn)
		c.Assert(err, qt.IsNil)
		root := csp.CensusRoot().Root
		c.Assert(len(root), qt.Equals, types.CensusRootLength)
		rawRoot, err := pubKeyPointToCensusRoot(DefaultHashFn, csp.privKey.Public())
		c.Assert(err, qt.IsNil)
		c.Assert(rawRoot.BigInt().String(), qt.Equals, root.BigInt().String())
	}
}
