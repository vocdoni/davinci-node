package ethereum

import (
	"encoding/hex"
	"testing"

	qt "github.com/frankban/quicktest"
)

func TestSignKeysGeneration(t *testing.T) {
	c := qt.New(t)
	t.Parallel()

	s := NewSignKeys(nil)
	c.Assert(s.Generate(), qt.IsNil)

	pub, priv := s.HexString()
	c.Assert(pub, qt.Not(qt.Equals), "")
	c.Assert(priv, qt.Not(qt.Equals), "")

	// Test key import
	imported := NewSignKeys(nil)
	c.Assert(imported.SetHexKey(priv), qt.IsNil)

	importedPub, importedPriv := imported.HexString()
	c.Assert(importedPub, qt.Equals, pub)
	c.Assert(importedPriv, qt.Equals, priv)
}

func TestEthereumSigning(t *testing.T) {
	c := qt.New(t)
	t.Parallel()

	// Test vector with known private key and expected signature
	testVector := struct {
		privKey           string
		message           []byte
		expectedSignature string
	}{
		privKey:           "fad9c8855b740a0b7ed4c221dbad0f33a83a49cad6b3fe8d5817ac83d38b6a19",
		message:           []byte("hello"),
		expectedSignature: "a0d0ebc374d2a4d6357eaca3da2f5f3ff547c3560008206bc234f9032a866ace6279ffb4093fb39c8bbc39021f6a5c36ef0e813c8c94f325a53f4f395a5c82de01",
	}

	// Create signing keys from known private key
	s := NewSignKeys(nil)
	c.Assert(s.SetHexKey(testVector.privKey), qt.IsNil)

	// Verify private key was imported correctly
	_, priv := s.HexString()
	c.Assert(priv, qt.Equals, testVector.privKey)

	// Sign message and verify signature matches expected
	signature, err := s.SignEthereum(testVector.message)
	c.Assert(err, qt.IsNil)

	expectedSig, err := hex.DecodeString(testVector.expectedSignature)
	c.Assert(err, qt.IsNil)
	c.Assert(signature, qt.DeepEquals, expectedSig)
}

func TestAddressRecovery(t *testing.T) {
	c := qt.New(t)
	t.Parallel()

	testCases := []struct {
		name    string
		message []byte
	}{
		{
			name:    "simple message",
			message: []byte("hello vocdoni"),
		},
		{
			name:    "different message",
			message: []byte("bye-bye vocdoni"),
		},
	}

	// Generate keys
	s := NewSignKeys(nil)
	c.Assert(s.Generate(), qt.IsNil)

	// Get address from public key
	expectedAddr, err := AddrFromPublicKey(s.PublicKey())
	c.Assert(err, qt.IsNil)
	c.Assert(expectedAddr.String(), qt.Equals, s.AddressString())

	// Test address recovery from signatures of different messages
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := qt.New(t)

			signature, err := s.SignEthereum(tc.message)
			c.Assert(err, qt.IsNil)

			recoveredAddr, err := AddrFromSignature(tc.message, signature)
			c.Assert(err, qt.IsNil)
			c.Assert(recoveredAddr, qt.Equals, expectedAddr)
		})
	}
}
