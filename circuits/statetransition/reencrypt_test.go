package statetransition

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/vocdoni/davinci-node/circuits"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/types"
)

type ReencryptedBallotCircuit struct {
	Originals         [types.FieldsPerBallot]frontend.Variable
	EncryptionKey     circuits.EncryptionKey[frontend.Variable]
	DecryptionKey     frontend.Variable
	ReencryptK        frontend.Variable
	EncryptedBallot   circuits.Ballot
	ReencryptedBallot circuits.Ballot
}

func (c *ReencryptedBallotCircuit) Define(api frontend.API) error {
	reencryptedBallot, _, _ := c.EncryptedBallot.Reencrypt(api, c.EncryptionKey, c.ReencryptK)
	c.ReencryptedBallot.AssertIsEqual(api, reencryptedBallot)
	reencryptedBallot.AssertDecrypt(api, c.DecryptionKey, c.Originals)
	return nil
}

func TestReencryptedBallotCircuit(t *testing.T) {
	c := qt.New(t)

	privkey := babyjub.NewRandPrivKey()

	x, y := format.FromTEtoRTE(privkey.Public().X, privkey.Public().Y)
	ek := new(bjj.BJJ).SetPoint(x, y)
	encKey := circuits.EncryptionKeyFromECCPoint(ek)
	encryptionKey := new(bjj.BJJ).SetPoint(encKey.PubKey[0], encKey.PubKey[1])

	k, err := elgamal.RandK()
	c.Assert(err, qt.IsNil)

	// generate fields
	fields := [types.FieldsPerBallot]*big.Int{}
	originals := [types.FieldsPerBallot]frontend.Variable{}
	for i := range fields {
		fields[i] = big.NewInt(int64(i + 1))
		originals[i] = frontend.Variable(fields[i])
	}

	ballot, err := elgamal.NewBallot(ek).Encrypt(fields, encryptionKey, k)
	c.Assert(err, qt.IsNil)
	// ballot = elgamal.NewBallot(ek)

	reencryptionK, err := elgamal.RandK()
	c.Assert(err, qt.IsNil)
	reencryptedBallot, _, err := ballot.Reencrypt(encryptionKey, reencryptionK)
	c.Assert(err, qt.IsNil)

	witness := &ReencryptedBallotCircuit{
		Originals:         originals,
		EncryptionKey:     encKey.AsVar(),
		DecryptionKey:     privkey.Scalar().BigInt(),
		ReencryptK:        reencryptionK,
		EncryptedBallot:   *ballot.ToGnark(),
		ReencryptedBallot: *reencryptedBallot.ToGnark(),
	}
	// generate proof
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&ReencryptedBallotCircuit{}, witness,
		test.WithCurves(ecc.BN254),
		test.WithBackends(backend.GROTH16))
}
