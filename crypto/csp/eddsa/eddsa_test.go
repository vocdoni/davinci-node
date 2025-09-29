package eddsa

import (
	"math/rand"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/twistededwards"
	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

func TestGenerateVerifyProof(t *testing.T) {
	c := qt.New(t)

	orgAddress := common.Address(util.RandomBytes(20))
	userAddress := common.Address(util.RandomBytes(20))

	processID := &types.ProcessID{
		Address: orgAddress,
		Nonce:   rand.Uint64(),
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	csp, err := CSP(twistededwards.BLS12_377)
	c.Assert(err, qt.IsNil)

	t.Run("invalid inputs", func(t *testing.T) {
		_, err := new(EdDSA).GenerateProof(processID, userAddress)
		c.Assert(err, qt.IsNotNil)

		_, err = csp.GenerateProof(nil, userAddress)
		c.Assert(err, qt.IsNotNil)

		_, err = csp.GenerateProof(&types.ProcessID{}, userAddress)
		c.Assert(err, qt.IsNotNil)

		_, err = csp.GenerateProof(processID, common.Address{})
		c.Assert(err, qt.IsNotNil)
	})

	t.Run("valid proof generation and verification", func(t *testing.T) {
		proof, err := csp.GenerateProof(processID, userAddress)
		c.Assert(err, qt.IsNil)
		c.Assert(proof, qt.IsNotNil)

		err = csp.VerifyProof(proof)
		c.Assert(err, qt.IsNil)
	})

	t.Run("invalid proof pubkey", func(t *testing.T) {
		proof, err := csp.GenerateProof(processID, userAddress)
		c.Assert(err, qt.IsNil)
		c.Assert(proof, qt.IsNotNil)

		proof.PublicKey = util.RandomBytes(20)
		err = csp.VerifyProof(proof)
		c.Assert(err, qt.IsNotNil)
	})

	t.Run("invalid proof address", func(t *testing.T) {
		proof, err := csp.GenerateProof(processID, userAddress)
		c.Assert(err, qt.IsNil)
		c.Assert(proof, qt.IsNotNil)

		proof.Address = util.RandomBytes(20)
		err = csp.VerifyProof(proof)
		c.Assert(err, qt.IsNotNil)
	})

	t.Run("invalid proof signature", func(t *testing.T) {
		proof, err := csp.GenerateProof(processID, userAddress)
		c.Assert(err, qt.IsNil)
		c.Assert(proof, qt.IsNotNil)

		proof.Signature = util.RandomBytes(20)
		err = csp.VerifyProof(proof)
		c.Assert(err, qt.IsNotNil)
	})
}
