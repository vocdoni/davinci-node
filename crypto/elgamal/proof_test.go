package elgamal

import (
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"

	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/curves"
)

func TestDecryptionProof(t *testing.T) {
	c := qt.New(t)
	curve := curves.New(bjj.CurveType)

	// Positive case

	pk, sk, err := GenerateKey(curve)
	c.Assert(err, qt.IsNil)

	msg := big.NewInt(42)

	c1, c2, _, err := Encrypt(pk, msg)
	c.Assert(err, qt.IsNil)

	msg2 := big.NewInt(8)
	c3, c4, _, err := Encrypt(pk, msg2)
	c.Assert(err, qt.IsNil)

	c1.Add(c1, c3)
	c2.Add(c2, c4)

	_, msgSumDecrypt, err := Decrypt(pk, sk, c1, c2, 1000)
	c.Assert(err, qt.IsNil)
	c.Assert(msgSumDecrypt.Cmp(big.NewInt(50)) == 0, qt.IsTrue, qt.Commentf("decrypted message must match original"))

	proof, err := BuildDecryptionProof(sk, pk, c1, c2, msgSumDecrypt)
	c.Assert(err, qt.IsNil)

	err = VerifyDecryptionProof(pk, c1, c2, msgSumDecrypt, proof)
	c.Assert(err, qt.IsNil, qt.Commentf("proof must verify for correct data"))

	//  Negative cases (should fail)

	// 1) Wrong plaintext
	wrongMsg := new(big.Int).Add(msg, big.NewInt(1))
	wrongMsg.Mod(wrongMsg, curve.Order())

	err = VerifyDecryptionProof(pk, c1, c2, wrongMsg, proof)
	c.Assert(err, qt.Not(qt.IsNil), qt.Commentf("verification should fail with wrong msg"))

	// 2) Tampered Z
	badProof := proof
	badProof.Z = new(big.Int).Add(proof.Z, big.NewInt(1))
	badProof.Z.Mod(badProof.Z, curve.Order())

	err = VerifyDecryptionProof(pk, c1, c2, msg, badProof)
	c.Assert(err, qt.Not(qt.IsNil), qt.Commentf("verification should fail with wrong Z"))

	// 3) Tampered A1
	badProof2 := proof
	badProof2.A1 = proof.A1.New()
	badProof2.A1.Set(proof.A1)
	// add generator to A1 (guaranteed change)
	tmp := proof.A1.New()
	tmp.SetGenerator()
	badProof2.A1.Add(badProof2.A1, tmp)

	err = VerifyDecryptionProof(pk, c1, c2, msg, badProof2)
	c.Assert(err, qt.Not(qt.IsNil), qt.Commentf("verification should fail with wrong A1"))
}
