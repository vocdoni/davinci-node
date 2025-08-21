package ethereum

import (
	"crypto/ecdsa"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/util"
)

func TestNewSigner(t *testing.T) {
	c := qt.New(t)

	// Create a new signer
	signer, err := NewSigner()
	c.Assert(err, qt.IsNil)
	c.Assert(signer, qt.Not(qt.IsNil))

	// Check the type conversion works properly
	privKey := (*ecdsa.PrivateKey)(signer)
	c.Assert(privKey, qt.Not(qt.IsNil))
	c.Assert(privKey.D, qt.Not(qt.IsNil))
	c.Assert(privKey.X, qt.Not(qt.IsNil))
	c.Assert(privKey.Y, qt.Not(qt.IsNil))
}

func TestNewSignerFromHex(t *testing.T) {
	c := qt.New(t)

	// Generate a private key
	privKey, err := ethcrypto.GenerateKey()
	c.Assert(err, qt.IsNil)

	// Convert to hex
	hexKey := ethcrypto.FromECDSA(privKey)
	hexKeyString := common.Bytes2Hex(hexKey)

	// Create signer from hex
	signer, err := NewSignerFromHex(hexKeyString)
	c.Assert(err, qt.IsNil)
	c.Assert(signer, qt.Not(qt.IsNil))

	// Check the private key matches
	originalAddress := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	signerAddress := signer.Address()
	c.Assert(signerAddress, qt.Equals, originalAddress)

	// Test with invalid hex
	_, err = NewSignerFromHex("invalid hex string")
	c.Assert(err, qt.Not(qt.IsNil))

	// Test with too short hex
	_, err = NewSignerFromHex("1234")
	c.Assert(err, qt.Not(qt.IsNil))
}

func TestSign(t *testing.T) {
	c := qt.New(t)

	// Generate a private key
	privKey, err := ethcrypto.GenerateKey()
	c.Assert(err, qt.IsNil)

	// Sign a message
	message := []byte("test message for sign function")
	signature, err := Sign(message, privKey)
	c.Assert(err, qt.IsNil)
	c.Assert(signature, qt.Not(qt.IsNil))
	c.Assert(signature.Valid(), qt.IsTrue)

	// Verify the signature
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	ok, _ := signature.Verify(message, address)
	c.Assert(ok, qt.IsTrue)

	// Try to recover the address from the signature
	recoveredAddr, err := AddrFromSignature(message, signature)
	c.Assert(err, qt.IsNil)
	expectedAddr := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	c.Assert(recoveredAddr, qt.Equals, expectedAddr)
}

func TestNewSignerFromSeed(t *testing.T) {
	c := qt.New(t)

	seed := util.RandomBytes(64)
	signer, err := NewSignerFromSeed(seed)
	c.Assert(err, qt.IsNil)

	msg := util.RandomBytes(32)
	signature, err := signer.Sign(msg)
	c.Assert(err, qt.IsNil)
	c.Assert(signature, qt.Not(qt.IsNil))

	// Verify the signature
	ok, _ := signature.Verify(msg, signer.Address())
	c.Assert(ok, qt.IsTrue)
}
