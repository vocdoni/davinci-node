package statetransition

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/types"
)

type ReencryptedBallotCircuit struct {
	Originals         [types.FieldsPerBallot]frontend.Variable
	EncryptionKey     circuits.EncryptionKey[frontend.Variable]
	DecryptionKey     frontend.Variable
	ReencryptionK     frontend.Variable
	EncryptedBallot   circuits.Ballot
	ReencryptedBallot circuits.Ballot
}

func (c *ReencryptedBallotCircuit) Define(api frontend.API) error {
	reencryptedBallot, _, _ := c.EncryptedBallot.Reencrypt(api, c.EncryptionKey, c.ReencryptionK)
	c.ReencryptedBallot.AssertIsEqual(api, reencryptedBallot)
	reencryptedBallot.AssertDecrypt(api, c.DecryptionKey, c.Originals)
	return nil
}

func TestReencryptedBallotCircuit(t *testing.T) {
	c := qt.New(t)

	privkey, encKey := testutil.RandomEncryptionKey()
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

	ballot, err := elgamal.NewBallot(encryptionKey).Encrypt(fields, encryptionKey, k)
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
		ReencryptionK:     reencryptionK,
		EncryptedBallot:   *ballot.ToGnark(),
		ReencryptedBallot: *reencryptedBallot.ToGnark(),
	}
	// generate proof
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&ReencryptedBallotCircuit{}, witness,
		test.WithCurves(ecc.BN254),
		test.WithBackends(backend.GROTH16))
}
