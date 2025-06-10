package secies

import (
	"crypto/rand"
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
)

func TestKeyGeneration(t *testing.T) {
	c := qt.New(t)
	curvePoint := curves.New(bjj.CurveType)

	se, err := New(nil, curvePoint, nil)
	c.Assert(err, qt.IsNil)
	c.Assert(se.privateKey, qt.Not(qt.IsNil))
	c.Assert(se.publicKey, qt.Not(qt.IsNil))
	c.Assert(se.privateKey.Sign(), qt.Not(qt.Equals), 0)

	zero := se.curvePoint.New()
	zero.SetZero()
	c.Assert(se.publicKey.Equal(zero), qt.IsFalse)
}

func TestEncryptionDecryption(t *testing.T) {
	c := qt.New(t)
	curvePoint := curves.New(bjj.CurveType)

	// Generate recipient keys
	recipient, err := New(nil, curvePoint, nil)
	c.Assert(err, qt.IsNil)

	recipientPublicKey := recipient.publicKey

	// Messages to test
	messages := []*big.Int{
		big.NewInt(0),
		big.NewInt(1),
		big.NewInt(-1),
		big.NewInt(42),
		new(big.Int).Sub(recipient.curvePoint.Order(), big.NewInt(1)), // Order - 1
		recipient.curvePoint.Order(),                                  // Exactly the order
		new(big.Int).Add(recipient.curvePoint.Order(), big.NewInt(1)), // Order + 1
	}

	for _, message := range messages {
		// Sender generates their own keys
		sender, err := New(nil, curvePoint, nil)
		c.Assert(err, qt.IsNil)

		// Encrypt the message
		ciphertext, RBytes, err := sender.Encrypt(new(big.Int).Set(message), recipientPublicKey)
		c.Assert(err, qt.IsNil)

		// Decrypt the ciphertext
		plaintext, err := recipient.Decrypt(ciphertext, RBytes)
		c.Assert(err, qt.IsNil)

		// Messages should be congruent modulo the curve order
		expectedMessage := new(big.Int).Mod(message, recipient.curvePoint.Order())
		c.Assert(plaintext.Cmp(expectedMessage), qt.Equals, 0)
	}
}

func TestDecryptionWithWrongPrivateKey(t *testing.T) {
	c := qt.New(t)
	curvePoint := curves.New(bjj.CurveType)

	// Generate recipient keys
	recipient, err := New(nil, curvePoint, nil)
	c.Assert(err, qt.IsNil)

	// Generate wrong recipient keys
	wrongRecipient, err := New(nil, curvePoint, nil)
	c.Assert(err, qt.IsNil)

	recipientPublicKey := recipient.publicKey
	message := big.NewInt(123456789)

	// Sender generates their own keys
	sender, err := New(nil, curvePoint, nil)
	c.Assert(err, qt.IsNil)

	// Encrypt the message
	ciphertext, RBytes, err := sender.Encrypt(message, recipientPublicKey)
	c.Assert(err, qt.IsNil)

	// Attempt decryption with the wrong private key
	recoveredMessage, err := wrongRecipient.Decrypt(ciphertext, RBytes)
	c.Assert(err, qt.IsNil)
	c.Assert(recoveredMessage.Cmp(message), qt.Not(qt.Equals), 0)
}

func TestDecryptionWithMalformedCiphertext(t *testing.T) {
	c := qt.New(t)
	curvePoint := curves.New(bjj.CurveType)

	// Generate recipient keys
	recipient, err := New(nil, curvePoint, nil)
	c.Assert(err, qt.IsNil)

	recipientPublicKey := recipient.publicKey
	message := big.NewInt(42)

	// Sender generates their own keys
	sender, err := New(nil, curvePoint, nil)
	c.Assert(err, qt.IsNil)

	// Encrypt the message
	ciphertext, RBytes, err := sender.Encrypt(message, recipientPublicKey)
	c.Assert(err, qt.IsNil)

	// Test corrupted ciphertext
	corruptedCiphertext := new(big.Int).Add(ciphertext, big.NewInt(1))
	recoveredMessage, err := recipient.Decrypt(corruptedCiphertext, RBytes)
	c.Assert(err, qt.IsNil)
	c.Assert(recoveredMessage.Cmp(message), qt.Not(qt.Equals), 0)

	// Test corrupted RBytes
	if len(RBytes) > 0 {
		corruptedRBytes := make([]byte, len(RBytes))
		copy(corruptedRBytes, RBytes)
		corruptedRBytes[0] ^= 0xFF
		recoveredMessage, err = recipient.Decrypt(ciphertext, corruptedRBytes)
		c.Assert(err, qt.IsNil)
		c.Assert(recoveredMessage.Cmp(message), qt.Not(qt.Equals), 0)
	}
}

func TestEncryptDecryptWithMaxMessage(t *testing.T) {
	c := qt.New(t)
	curvePoint := curves.New(bjj.CurveType)

	// Generate recipient keys
	recipient, err := New(nil, curvePoint, nil)
	c.Assert(err, qt.IsNil)

	recipientPublicKey := recipient.publicKey
	maxMessage := new(big.Int).Sub(curvePoint.Order(), big.NewInt(1))

	// Sender generates their own keys
	sender, err := New(nil, curvePoint, nil)
	c.Assert(err, qt.IsNil)

	// Encrypt the message
	ciphertext, RBytes, err := sender.Encrypt(maxMessage, recipientPublicKey)
	c.Assert(err, qt.IsNil)

	// Decrypt the ciphertext
	plaintext, err := recipient.Decrypt(ciphertext, RBytes)
	c.Assert(err, qt.IsNil)
	c.Assert(plaintext.Cmp(maxMessage), qt.Equals, 0)
}

func TestEncryptDecryptRandomMessages(t *testing.T) {
	c := qt.New(t)
	curvePoint := curves.New(bjj.CurveType)

	// Generate recipient keys
	recipient, err := New(nil, curvePoint, nil)
	c.Assert(err, qt.IsNil)

	recipientPublicKey := recipient.publicKey
	numMessages := 100

	for i := 0; i < numMessages; i++ {
		// Generate a random message
		message, err := rand.Int(rand.Reader, curvePoint.Order())
		c.Assert(err, qt.IsNil)

		// Sender generates their own keys
		sender, err := New(nil, curvePoint, nil)
		c.Assert(err, qt.IsNil)

		// Encrypt the message
		ciphertext, RBytes, err := sender.Encrypt(message, recipientPublicKey)
		c.Assert(err, qt.IsNil)

		// Decrypt the ciphertext
		plaintext, err := recipient.Decrypt(ciphertext, RBytes)
		c.Assert(err, qt.IsNil)
		c.Assert(plaintext.Cmp(message), qt.Equals, 0)
	}
}

func TestEncryptWithNilPrivateKey(t *testing.T) {
	c := qt.New(t)
	curvePoint := curves.New(bjj.CurveType)

	// Create a recipient with nil private key (should generate keys)
	se, err := New(nil, curvePoint, nil)
	c.Assert(err, qt.IsNil)
	c.Assert(se.privateKey, qt.Not(qt.IsNil))
	c.Assert(se.publicKey, qt.Not(qt.IsNil))
}

func TestPublicKeyMarshaling(t *testing.T) {
	c := qt.New(t)
	curvePoint := curves.New(bjj.CurveType)

	// Generate keys
	se, err := New(nil, curvePoint, nil)
	c.Assert(err, qt.IsNil)

	publicKeyBytes := se.GetPublicKey()

	// Unmarshal the public key
	publicKey := curvePoint.New()
	c.Assert(publicKey.Unmarshal(publicKeyBytes), qt.IsNil)
	c.Assert(publicKey.Equal(se.publicKey), qt.IsTrue)
}
