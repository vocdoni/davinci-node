package elgamal

import (
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bn254"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/curves"
)

func TestGenerateKey(t *testing.T) {
	c := qt.New(t)
	curve := curves.New(bn254.CurveType)

	publicKey, privateKey, err := GenerateKey(curve)
	c.Assert(err, qt.IsNil)
	c.Assert(publicKey, qt.Not(qt.IsNil))
	c.Assert(privateKey, qt.Not(qt.IsNil))

	// Check if publicKey = privateKey * G
	testPoint := curve.New()
	testPoint.SetGenerator()
	testPoint.ScalarMult(testPoint, privateKey)
	c.Assert(testPoint.Equal(publicKey), qt.IsTrue)
}

func TestEncryptDecrypt(t *testing.T) {
	c := qt.New(t)
	curve := curves.New(bn254.CurveType)

	publicKey, privateKey, err := GenerateKey(curve)
	c.Assert(err, qt.IsNil)

	maxMessage := uint64(1000)

	for _, m := range []uint64{0, 1, 42, 999} {
		msg := big.NewInt(int64(m))
		c1, c2, k, err := Encrypt(publicKey, msg)
		c.Assert(err, qt.IsNil)
		c.Assert(k, qt.Not(qt.IsNil))

		M, recoveredMsg, err := Decrypt(publicKey, privateKey, c1, c2, maxMessage)
		c.Assert(err, qt.IsNil)
		c.Assert(recoveredMsg.String(), qt.DeepEquals, msg.String())

		// Check M = m * G
		testPoint := curve.New()
		testPoint.SetGenerator()
		testPoint.ScalarMult(testPoint, msg)
		c.Assert(testPoint.Equal(M), qt.IsTrue)
	}
}

func TestCheckK(t *testing.T) {
	c := qt.New(t)

	// Setup a curve point
	curve := curves.New(bn254.CurveType)

	// Generate a key pair
	pubKey, privKey, err := GenerateKey(curve)
	c.Assert(err, qt.IsNil)

	// Define a message
	msg := big.NewInt(42)
	maxMsg := uint64(100)

	// Encrypt the message
	c1, c2, k, err := Encrypt(pubKey, msg)
	c.Assert(err, qt.IsNil)

	// Check that k is indeed the one used
	c.Assert(CheckK(c1, k), qt.IsTrue, qt.Commentf("CheckK failed: it should have found that this k was used"))

	// Try a wrong k
	wrongK := big.NewInt(999999) // a random different k
	c.Assert(CheckK(c1, wrongK), qt.IsFalse, qt.Commentf("CheckK failed: it should not have matched this wrong k"))

	// Bonus: try decrypting to ensure correctness (not necessary for CheckK logic)
	M, mInt, err := Decrypt(pubKey, privKey, c1, c2, maxMsg)
	c.Assert(err, qt.IsNil)
	c.Assert(mInt.Cmp(msg), qt.Equals, 0, qt.Commentf("Decryption mismatch: got %s, want %s", mInt.String(), msg.String()))
	c.Assert(M, qt.Not(qt.IsNil), qt.Commentf("M point is nil after decrypt"))
}
