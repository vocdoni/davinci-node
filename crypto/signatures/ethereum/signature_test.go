package ethereum

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	qt "github.com/frankban/quicktest"
)

func TestNew(t *testing.T) {
	c := qt.New(t)

	// Generate a test signature
	privKey, err := ethcrypto.GenerateKey()
	c.Assert(err, qt.IsNil)

	msg := []byte("test message")
	ethSig, err := ethcrypto.Sign(HashMessage(msg), privKey)
	c.Assert(err, qt.IsNil)
	c.Assert(len(ethSig), qt.Equals, SignatureLength)

	// Test creating new signature from valid data
	sig, err := New(ethSig)
	c.Assert(err, qt.IsNil)
	c.Assert(sig, qt.Not(qt.IsNil))
	c.Assert(sig.R, qt.Not(qt.IsNil))
	c.Assert(sig.S, qt.Not(qt.IsNil))
	c.Assert(sig.recovery, qt.Equals, ethSig[64])

	// Test invalid signature (too short)
	shortSig := ethSig[:SignatureMinLength-1]
	_, err = New(shortSig)
	c.Assert(err, qt.Not(qt.IsNil))
}

func TestECDSASignature_Valid(t *testing.T) {
	c := qt.New(t)

	// Valid signature
	validSig := &ECDSASignature{
		R: big.NewInt(123),
		S: big.NewInt(456),
	}
	c.Assert(validSig.Valid(), qt.IsTrue)

	// Invalid signature - R is nil
	invalidSig1 := &ECDSASignature{
		R: nil,
		S: big.NewInt(456),
	}
	c.Assert(invalidSig1.Valid(), qt.IsFalse)

	// Invalid signature - S is nil
	invalidSig2 := &ECDSASignature{
		R: big.NewInt(123),
		S: nil,
	}
	c.Assert(invalidSig2.Valid(), qt.IsFalse)

	// Invalid signature - both R and S are nil
	invalidSig3 := &ECDSASignature{
		R: nil,
		S: nil,
	}
	c.Assert(invalidSig3.Valid(), qt.IsFalse)
}

func TestECDSASignature_Bytes(t *testing.T) {
	c := qt.New(t)

	// Create a signature with known values
	sig := &ECDSASignature{
		R:        big.NewInt(123),
		S:        big.NewInt(456),
		recovery: 1,
	}

	bytes := sig.Bytes()
	c.Assert(len(bytes), qt.Equals, SignatureLength)

	// Check padding for R and S
	r := bytes[:32]
	s := bytes[32:64]
	recovery := bytes[64]

	// Check values
	c.Assert(new(big.Int).SetBytes(r).Cmp(sig.R), qt.Equals, 0)
	c.Assert(new(big.Int).SetBytes(s).Cmp(sig.S), qt.Equals, 0)
	c.Assert(recovery, qt.Equals, sig.recovery)

	// Create a new signature from these bytes
	recoveredSig, err := New(bytes)
	c.Assert(err, qt.IsNil)
	c.Assert(recoveredSig.R.Cmp(sig.R), qt.Equals, 0)
	c.Assert(recoveredSig.S.Cmp(sig.S), qt.Equals, 0)
	c.Assert(recoveredSig.recovery, qt.Equals, sig.recovery)
}

func TestECDSASignature_Verify(t *testing.T) {
	c := qt.New(t)

	// Generate a keypair
	privKey, err := ethcrypto.GenerateKey()
	c.Assert(err, qt.IsNil)
	pubKey := ethcrypto.FromECDSAPub(&privKey.PublicKey)

	// Sign a message
	msg := []byte("test verification message")
	ethSig, err := ethcrypto.Sign(HashMessage(msg), privKey)
	c.Assert(err, qt.IsNil)

	// Create signature
	sig, err := New(ethSig)
	c.Assert(err, qt.IsNil)

	// Manual verification using ethereum's native functions
	verifyBytes := sig.Bytes()
	c.Assert(ethcrypto.VerifySignature(pubKey, HashMessage(msg), verifyBytes[:64]), qt.IsTrue)

	// Check our wrapper matches ethereum's native function
	c.Assert(sig.Verify(msg, pubKey), qt.Equals, ethcrypto.VerifySignature(pubKey, HashMessage(msg), verifyBytes[:64]))

	// Verify with wrong message
	wrongMsg := []byte("wrong message")
	c.Assert(ethcrypto.VerifySignature(pubKey, HashMessage(wrongMsg), verifyBytes[:64]), qt.IsFalse)
	c.Assert(sig.Verify(wrongMsg, pubKey), qt.IsFalse)

	// Verify with wrong public key
	wrongPrivKey, err := ethcrypto.GenerateKey()
	c.Assert(err, qt.IsNil)
	wrongPubKey := ethcrypto.FromECDSAPub(&wrongPrivKey.PublicKey)
	c.Assert(ethcrypto.VerifySignature(wrongPubKey, HashMessage(msg), verifyBytes[:64]), qt.IsFalse)
	c.Assert(sig.Verify(msg, wrongPubKey), qt.IsFalse)

	// Test invalid signature
	invalidSig := &ECDSASignature{
		R: nil,
		S: big.NewInt(456),
	}
	c.Assert(invalidSig.Valid(), qt.IsFalse)
	c.Assert(invalidSig.Verify(msg, pubKey), qt.IsFalse)
}

func TestAddrFromSignature(t *testing.T) {
	c := qt.New(t)

	// Generate a keypair
	privKey, err := ethcrypto.GenerateKey()
	c.Assert(err, qt.IsNil)
	expectedAddr := ethcrypto.PubkeyToAddress(privKey.PublicKey)

	// Sign a message
	msg := []byte("test address recovery")
	ethSig, err := ethcrypto.Sign(HashMessage(msg), privKey)
	c.Assert(err, qt.IsNil)

	// Recover address
	addr, err := AddrFromSignature(msg, ethSig)
	c.Assert(err, qt.IsNil)
	c.Assert(addr, qt.Equals, expectedAddr)

	// Test with invalid signature (too short)
	_, err = AddrFromSignature(msg, ethSig[:SignatureMinLength-1])
	c.Assert(err, qt.Not(qt.IsNil))

	// Test with invalid recovery ID
	invalidSig := make([]byte, len(ethSig))
	copy(invalidSig, ethSig)
	invalidSig[64] = 99 // Invalid recovery ID
	_, err = AddrFromSignature(msg, invalidSig)
	c.Assert(err, qt.Not(qt.IsNil))
}

func TestWebBrowserSignatureVerification(t *testing.T) {
	c := qt.New(t)

	// Test data provided for web browser signature verification
	message := []byte("Hello world!")
	signatureHex := "0x4fe294db29ddda38c1a4d170db34adc0f7431d7b0cbb0ae8adb6b4ea94f1bde159352a6ab3c16f62b5fa3d84bfc21d65aa2aacb3a841034f928053b4a6fcf7c21c"
	expectedAddr := common.HexToAddress("0xbF7b6386ECb6b8bFCc548D2C51F142a513DEb752")

	// Remove the '0x' prefix if present
	signatureHex = strings.TrimPrefix(signatureHex, "0x")

	// Decode signature
	signatureBytes, err := hex.DecodeString(signatureHex)
	c.Assert(err, qt.IsNil)
	c.Assert(len(signatureBytes), qt.Equals, SignatureLength)

	// Create signature object
	sig, err := New(signatureBytes)
	c.Assert(err, qt.IsNil)

	// Recover address from signature
	recoveredAddr, err := AddrFromSignature(message, signatureBytes)
	c.Assert(err, qt.IsNil)
	c.Assert(recoveredAddr, qt.Equals, expectedAddr)

	// Get public key from address
	// Note: We can't get the exact public key from an address, but we can verify
	// that the signature verifies against the message using the recovered address
	pubKey, err := ethcrypto.SigToPub(HashMessage(message), signatureBytes)
	c.Assert(err, qt.IsNil)
	recoveredAddrFromPubKey := ethcrypto.PubkeyToAddress(*pubKey)
	c.Assert(recoveredAddrFromPubKey, qt.Equals, expectedAddr)

	// Verify signature using ethereum's native function
	pubKeyBytes := ethcrypto.FromECDSAPub(pubKey)
	verifyBytes := sig.Bytes()[:64] // Exclude recovery byte
	c.Assert(ethcrypto.VerifySignature(pubKeyBytes, HashMessage(message), verifyBytes), qt.IsTrue)

	// Verify via our wrapper function
	c.Assert(sig.Verify(message, pubKeyBytes), qt.IsTrue)

	// Verify with wrong message
	wrongMessage := []byte("Wrong message!")
	c.Assert(sig.Verify(wrongMessage, pubKeyBytes), qt.IsFalse)
}
