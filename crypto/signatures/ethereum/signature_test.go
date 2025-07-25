package ethereum

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/log"
)

func init() {
	// Initialize the logger to avoid log output during tests
	log.Init("debug", "stdout", nil)
}

func TestNew(t *testing.T) {
	c := qt.New(t)

	// Generate a test signature
	privKey, err := ethcrypto.GenerateKey()
	c.Assert(err, qt.IsNil)

	msg := []byte("test message")
	ethSig, err := ethcrypto.Sign(HashMessage(msg), privKey)
	c.Assert(err, qt.IsNil)
	c.Assert(len(ethSig), qt.Equals, SignatureLength)

	t.Logf("PrivKey: %x", ethcrypto.FromECDSA(privKey))
	t.Logf("PubKey: %x", ethcrypto.FromECDSAPub(&privKey.PublicKey))
	t.Logf("Signature: %x", ethSig)
	t.Logf("Signature length: %d", len(ethSig))
	t.Logf("Message: %s", msg)
	t.Logf("Message hash: %x", HashMessage(msg))

	// Test creating new signature from valid data
	sig, err := New(ethSig)
	c.Assert(err, qt.IsNil)
	c.Assert(sig, qt.Not(qt.IsNil))
	c.Assert(sig.R, qt.Not(qt.IsNil))
	c.Assert(sig.S, qt.Not(qt.IsNil))
	c.Assert(sig.recovery, qt.Equals, ethSig[64])

	// Test invalid signature (too short)
	shortSig := ethSig[:SignatureLength-2]
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

	// Check values
	c.Assert(new(big.Int).SetBytes(r).Cmp(sig.R), qt.Equals, 0)
	c.Assert(new(big.Int).SetBytes(s).Cmp(sig.S), qt.Equals, 0)

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
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
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
	t.Logf("pubkey: %x", pubKey)
	ok, _ := sig.Verify(msg, address)
	t.Logf("verification: %t", ok)
	c.Assert(ok, qt.Equals, ethcrypto.VerifySignature(pubKey, HashMessage(msg), verifyBytes[:64]))

	// Verify with wrong message
	wrongMsg := []byte("wrong message")
	ok, _ = sig.Verify(wrongMsg, address)
	c.Assert(ethcrypto.VerifySignature(pubKey, HashMessage(wrongMsg), verifyBytes[:64]), qt.IsFalse)
	c.Assert(ok, qt.IsFalse)

	// Verify with wrong public key
	wrongPrivKey, err := ethcrypto.GenerateKey()
	c.Assert(err, qt.IsNil)
	wrongPubKey := ethcrypto.FromECDSAPub(&wrongPrivKey.PublicKey)
	wrongAddr := ethcrypto.PubkeyToAddress(wrongPrivKey.PublicKey)
	ok, _ = sig.Verify(msg, wrongAddr)
	c.Assert(ethcrypto.VerifySignature(wrongPubKey, HashMessage(msg), verifyBytes[:64]), qt.IsFalse)
	c.Assert(ok, qt.IsFalse)

	// Test invalid signature
	invalidSig := &ECDSASignature{
		R: nil,
		S: big.NewInt(456),
	}
	c.Assert(invalidSig.Valid(), qt.IsFalse)
	ok, _ = invalidSig.Verify(msg, address)
	c.Assert(ok, qt.IsFalse)
}

func TestAddrFromSignature(t *testing.T) {
	c := qt.New(t)

	// Generate a keypair
	privKey, err := ethcrypto.GenerateKey()
	c.Assert(err, qt.IsNil)
	expectedAddr := ethcrypto.PubkeyToAddress(privKey.PublicKey)

	// Sign a message
	msg := []byte("test address recovery")
	ethSignature, err := ethcrypto.Sign(HashMessage(msg), privKey)
	c.Assert(err, qt.IsNil)

	ethSig := new(ECDSASignature).SetBytes(ethSignature)
	// Recover address
	addr, err := AddrFromSignature(msg, ethSig)
	c.Assert(err, qt.IsNil)
	c.Assert(addr, qt.Equals, expectedAddr)
}

func TestECDSASignature_SetBytesWebBrowserSignature(t *testing.T) {
	c := qt.New(t)

	// Use the web browser signature from TestWebBrowserSignatureVerification
	message := []byte("Hello world!")
	signatureHex := "0x4fe294db29ddda38c1a4d170db34adc0f7431d7b0cbb0ae8adb6b4ea94f1bde159352a6ab3c16f62b5fa3d84bfc21d65aa2aacb3a841034f928053b4a6fcf7c21c"
	expectedAddr := common.HexToAddress("0xbF7b6386ECb6b8bFCc548D2C51F142a513DEb752")

	// Remove the '0x' prefix if present
	signatureHex = strings.TrimPrefix(signatureHex, "0x")

	// Decode signature
	signatureBytes, err := hex.DecodeString(signatureHex)
	c.Assert(err, qt.IsNil)
	c.Assert(len(signatureBytes), qt.Equals, SignatureLength)

	// Test with 65-byte signature using SetBytes
	sig65 := &ECDSASignature{}
	result := sig65.SetBytes(signatureBytes)
	c.Assert(result, qt.Not(qt.IsNil))

	// Verify the address can be recovered
	recoveredAddr, err := AddrFromSignature(message, sig65)
	c.Assert(err, qt.IsNil)
	c.Assert(recoveredAddr, qt.Equals, expectedAddr)

	// Test with 64-byte signature (without recovery byte)
	sig64 := &ECDSASignature{
		R: new(big.Int),
		S: new(big.Int),
	}
	result = sig64.SetBytes(signatureBytes[:64])
	c.Assert(result, qt.Not(qt.IsNil))
	c.Assert(sig64.recovery, qt.Equals, byte(0))

	// Make sure the R and S components match between the 65-byte and 64-byte versions
	c.Assert(sig64.R.Cmp(sig65.R), qt.Equals, 0)
	c.Assert(sig64.S.Cmp(sig65.S), qt.Equals, 0)
}

// TestAddrFromClientSignature tests the recovery of an Ethereum address from a client-generated signature
func TestAddrFromClientSignature(t *testing.T) {
	c := qt.New(t)

	// Test data provided by the user
	payloadToSign := []byte("1115511163")
	signatureHex := "0xfc57ab89119a0fffecde10d9de81cf67ce7336301ee5d2f6eefea7c9489bca644eecb440da2c6d109f53677b5d75875c1207b53e4296cba8f3e3bb52904d77f91b"
	expectedAddr := common.HexToAddress("0xA62E32147e9c1EA76DA552Be6E0636F1984143AF")

	// Remove the '0x' prefix if present
	signatureHex = strings.TrimPrefix(signatureHex, "0x")

	// Decode signature
	signatureBytes, err := hex.DecodeString(signatureHex)
	c.Assert(err, qt.IsNil)
	c.Assert(len(signatureBytes), qt.Equals, SignatureLength)

	// Create signature object using SetBytes
	sig := &ECDSASignature{}
	result := sig.SetBytes(signatureBytes)
	c.Assert(result, qt.Not(qt.IsNil))

	// Print signature components for debugging
	t.Logf("Original signature: %s", hex.EncodeToString(signatureBytes))
	t.Logf("R: %s", sig.R.Text(16))
	t.Logf("S: %s", sig.S.Text(16))
	t.Logf("V: %d", sig.recovery)
	t.Logf("Reconstructed: %s", hex.EncodeToString(sig.Bytes()))

	// Attempt to recover the address
	recoveredAddr, err := AddrFromSignature(payloadToSign, sig)
	c.Assert(err, qt.IsNil)
	c.Assert(recoveredAddr, qt.Equals, expectedAddr)
}
