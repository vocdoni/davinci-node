package elgamal

import (
	"encoding/json"
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/fxamacker/cbor/v2"
	"github.com/vocdoni/davinci-node/crypto/ecc/bn254"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
)

func TestNewCiphertext(t *testing.T) {
	c := qt.New(t)

	cipher := NewCiphertext(curves.New(bn254.CurveType))
	c.Assert(cipher, qt.Not(qt.IsNil))
	c.Assert(cipher.C1, qt.Not(qt.IsNil))
	c.Assert(cipher.C2, qt.Not(qt.IsNil))
}

func TestCiphertext_Encrypt(t *testing.T) {
	c := qt.New(t)

	// Create a test key pair
	curve := curves.New(bn254.CurveType)
	publicKey, _, err := GenerateKey(curve)
	c.Assert(err, qt.IsNil)

	// Test message
	msg := big.NewInt(42)
	// Test with specific k
	cipher := NewCiphertext(publicKey)
	k := big.NewInt(789)
	encrypted2, err := cipher.Encrypt(msg, publicKey, k)
	c.Assert(err, qt.IsNil)
	c.Assert(encrypted2, qt.Not(qt.IsNil))
}

func TestCiphertext_Add(t *testing.T) {
	c := qt.New(t)

	// Create test ciphertexts
	curve := curves.New(bn254.CurveType)
	publicKey, _, err := GenerateKey(curve)
	c.Assert(err, qt.IsNil)

	msg1 := big.NewInt(42)
	msg2 := big.NewInt(58)

	k1 := big.NewInt(789)
	k2 := big.NewInt(987)

	cipher1 := NewCiphertext(publicKey)
	encrypted1, err := cipher1.Encrypt(msg1, publicKey, k1)
	c.Assert(err, qt.IsNil)

	cipher2 := NewCiphertext(publicKey)
	encrypted2, err := cipher2.Encrypt(msg2, publicKey, k2)
	c.Assert(err, qt.IsNil)

	// Test addition
	result := NewCiphertext(publicKey)
	// Initialize result points with the first ciphertext's values
	result.C1 = encrypted1.C1
	result.C2 = encrypted1.C2
	// Now add the second ciphertext
	sum := result.Add(result, encrypted2)
	c.Assert(sum, qt.Not(qt.IsNil))
	c.Assert(sum.C1, qt.Not(qt.IsNil))
	c.Assert(sum.C2, qt.Not(qt.IsNil))
}

func TestCiphertext_SerializeDeserialize(t *testing.T) {
	c := qt.New(t)

	// Create a test ciphertext
	curve := curves.New(bn254.CurveType)
	publicKey, _, err := GenerateKey(curve)
	c.Assert(err, qt.IsNil)

	msg := big.NewInt(42)
	k := big.NewInt(789)

	cipher := NewCiphertext(publicKey)
	encrypted, err := cipher.Encrypt(msg, publicKey, k)
	c.Assert(err, qt.IsNil)

	// Test serialization
	serialized := encrypted.Serialize()
	c.Assert(serialized, qt.Not(qt.IsNil))
	c.Assert(len(serialized), qt.Equals, 128) // 4 * 32 bytes

	// Test deserialization
	deserialized := NewCiphertext(publicKey)
	err = deserialized.Deserialize(serialized)
	c.Assert(err, qt.IsNil)

	// Compare points
	x1, y1 := encrypted.C1.Point()
	x2, y2 := deserialized.C1.Point()
	c.Assert(x1.Cmp(x2), qt.Equals, 0)
	c.Assert(y1.Cmp(y2), qt.Equals, 0)

	x1, y1 = encrypted.C2.Point()
	x2, y2 = deserialized.C2.Point()
	c.Assert(x1.Cmp(x2), qt.Equals, 0)
	c.Assert(y1.Cmp(y2), qt.Equals, 0)
}

func TestCiphertext_MarshalUnmarshal(t *testing.T) {
	c := qt.New(t)

	// Create a test ciphertext
	curve := curves.New(bn254.CurveType)
	publicKey, _, err := GenerateKey(curve)
	c.Assert(err, qt.IsNil)

	msg := big.NewInt(42)
	k := big.NewInt(789)

	cipher := NewCiphertext(publicKey)
	encrypted, err := cipher.Encrypt(msg, publicKey, k)
	c.Assert(err, qt.IsNil)

	// Test marshaling
	marshaled, err := json.Marshal(encrypted)
	c.Assert(err, qt.IsNil)
	c.Assert(marshaled, qt.Not(qt.IsNil))

	// Test unmarshaling
	unmarshaled := NewCiphertext(publicKey)
	err = json.Unmarshal(marshaled, unmarshaled)
	c.Assert(err, qt.IsNil)

	// Compare points
	x1, y1 := encrypted.C1.Point()
	x2, y2 := unmarshaled.C1.Point()
	c.Assert(x1.Cmp(x2), qt.Equals, 0)
	c.Assert(y1.Cmp(y2), qt.Equals, 0)

	x1, y1 = encrypted.C2.Point()
	x2, y2 = unmarshaled.C2.Point()
	c.Assert(x1.Cmp(x2), qt.Equals, 0)
	c.Assert(y1.Cmp(y2), qt.Equals, 0)
}

func TestCiphertext_String(t *testing.T) {
	c := qt.New(t)

	// Create a test ciphertext with properly initialized points
	curve := curves.New(bn254.CurveType)
	publicKey, _, err := GenerateKey(curve)
	c.Assert(err, qt.IsNil)

	msg := big.NewInt(42)
	k := big.NewInt(789)

	cipher := NewCiphertext(publicKey)
	encrypted, err := cipher.Encrypt(msg, publicKey, k)
	c.Assert(err, qt.IsNil)

	// Test String method
	str := encrypted.String()
	c.Assert(str, qt.Not(qt.Equals), "")
	c.Assert(str, qt.Matches, `\{C1: .+, C2: .+\}`)
}

func TestCiphertext_DeserializeError(t *testing.T) {
	c := qt.New(t)

	cipher := NewCiphertext(curves.New(bn254.CurveType))

	// Test with invalid length, should panic
	c.Assert(cipher.Deserialize(make([]byte, 127)), // Should be 128
		qt.ErrorMatches, "invalid input length.*")
}

func TestBallotMarshalCBOR(t *testing.T) {
	c := qt.New(t)

	// Create a test ciphertext for all curves
	for _, curveType := range curves.Curves() {
		curve := curves.New(curveType)
		publicKey, _, err := GenerateKey(curve)
		c.Assert(err, qt.IsNil)

		msg := big.NewInt(42)
		k := big.NewInt(789)

		ballot := NewBallot(curve)
		ballot, err = ballot.Encrypt([8]*big.Int{msg, msg, msg, msg, msg, msg, msg, msg}, publicKey, k)
		c.Assert(err, qt.IsNil)

		// Test marshaling
		marshaled, err := cbor.Marshal(ballot)
		c.Assert(err, qt.IsNil)
		c.Assert(marshaled, qt.Not(qt.IsNil))

		// Test unmarshaling
		unmarshaled := Ballot{}
		err = cbor.Unmarshal(marshaled, &unmarshaled)
		c.Assert(err, qt.IsNil)

		// Compare points
		for i := 0; i < len(ballot.Ciphertexts); i++ {
			x1, y1 := ballot.Ciphertexts[i].C1.Point()
			x2, y2 := unmarshaled.Ciphertexts[i].C1.Point()
			c.Assert(x1.Cmp(x2), qt.Equals, 0)
			c.Assert(y1.Cmp(y2), qt.Equals, 0)

			x1, y1 = ballot.Ciphertexts[i].C2.Point()
			x2, y2 = unmarshaled.Ciphertexts[i].C2.Point()
			c.Assert(x1.Cmp(x2), qt.Equals, 0)
			c.Assert(y1.Cmp(y2), qt.Equals, 0)
		}
	}
}

func TestBallotMarshalJSON(t *testing.T) {
	c := qt.New(t)

	// Create a test ciphertext for all curves.
	for _, curveType := range curves.Curves() {
		curve := curves.New(curveType)
		publicKey, _, err := GenerateKey(curve)
		c.Assert(err, qt.IsNil)

		msg := big.NewInt(42)
		k := big.NewInt(789)

		ballot := NewBallot(curve)
		ballot, err = ballot.Encrypt(
			[8]*big.Int{msg, msg, msg, msg, msg, msg, msg, msg},
			publicKey,
			k,
		)
		c.Assert(err, qt.IsNil)

		// Test JSON marshaling.
		marshaled, err := json.Marshal(ballot)
		c.Assert(err, qt.IsNil)
		c.Assert(marshaled, qt.Not(qt.IsNil))

		// Test JSON unmarshaling.
		unmarshaled := Ballot{}
		err = json.Unmarshal(marshaled, &unmarshaled)
		c.Assert(err, qt.IsNil)

		// Compare points for each ciphertext.
		for i := 0; i < len(ballot.Ciphertexts); i++ {
			// Compare C1.
			x1, y1 := ballot.Ciphertexts[i].C1.Point()
			x2, y2 := unmarshaled.Ciphertexts[i].C1.Point()
			c.Assert(x1.Cmp(x2), qt.Equals, 0)
			c.Assert(y1.Cmp(y2), qt.Equals, 0)

			// Compare C2.
			x1, y1 = ballot.Ciphertexts[i].C2.Point()
			x2, y2 = unmarshaled.Ciphertexts[i].C2.Point()
			c.Assert(x1.Cmp(x2), qt.Equals, 0)
			c.Assert(y1.Cmp(y2), qt.Equals, 0)
		}
	}
}
