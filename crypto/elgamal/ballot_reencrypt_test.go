package elgamal

import (
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/spec/params"
	specutil "github.com/vocdoni/davinci-node/spec/util"
)

func TestBallotReencryptUsesDistinctOffsetsPerField(t *testing.T) {
	c := qt.New(t)

	privKey, encKey := testutil.RandomEncryptionKeys()
	_ = privKey
	publicKey := new(bjj.BJJ).SetPoint(encKey.X.MathBigInt(), encKey.Y.MathBigInt())

	k, err := specutil.RandomK()
	c.Assert(err, qt.IsNil)

	fields := [params.FieldsPerBallot]*big.Int{}
	for i := range fields {
		fields[i] = big.NewInt(int64(i + 1))
	}

	ballot, err := NewBallot(publicKey).Encrypt(fields, publicKey, k)
	c.Assert(err, qt.IsNil)

	reencryptionSeed, err := specutil.RandomK()
	c.Assert(err, qt.IsNil)

	reencryptedBallot, _, err := ballot.Reencrypt(publicKey, reencryptionSeed)
	c.Assert(err, qt.IsNil)

	originalDiffC1 := publicKey.New()
	negOriginalC1 := publicKey.New()
	negOriginalC1.Neg(ballot.Ciphertexts[1].C1)
	originalDiffC1.Add(ballot.Ciphertexts[0].C1, negOriginalC1)

	reencryptedDiffC1 := publicKey.New()
	negReencryptedC1 := publicKey.New()
	negReencryptedC1.Neg(reencryptedBallot.Ciphertexts[1].C1)
	reencryptedDiffC1.Add(reencryptedBallot.Ciphertexts[0].C1, negReencryptedC1)

	c.Assert(reencryptedDiffC1.Equal(originalDiffC1), qt.IsFalse, qt.Commentf("C1 field differences should change after reencryption"))

	originalDiffC2 := publicKey.New()
	negOriginalC2 := publicKey.New()
	negOriginalC2.Neg(ballot.Ciphertexts[1].C2)
	originalDiffC2.Add(ballot.Ciphertexts[0].C2, negOriginalC2)

	reencryptedDiffC2 := publicKey.New()
	negReencryptedC2 := publicKey.New()
	negReencryptedC2.Neg(reencryptedBallot.Ciphertexts[1].C2)
	reencryptedDiffC2.Add(reencryptedBallot.Ciphertexts[0].C2, negReencryptedC2)

	c.Assert(reencryptedDiffC2.Equal(originalDiffC2), qt.IsFalse, qt.Commentf("C2 field differences should change after reencryption"))
}

func TestZeroBallotReencryptChangesBallot(t *testing.T) {
	c := qt.New(t)

	_, encKey := testutil.RandomEncryptionKeys()
	publicKey := new(bjj.BJJ).SetPoint(encKey.X.MathBigInt(), encKey.Y.MathBigInt())

	zeroBallot := NewBallot(publicKey)
	c.Assert(zeroBallot.IsZero(), qt.IsTrue)

	reencryptionSeed, err := specutil.RandomK()
	c.Assert(err, qt.IsNil)

	reencryptedBallot, _, err := zeroBallot.Reencrypt(publicKey, reencryptionSeed)
	c.Assert(err, qt.IsNil)
	c.Assert(reencryptedBallot.IsZero(), qt.IsFalse, qt.Commentf("reencryption should add encrypted zero even for zero ballots"))
}
