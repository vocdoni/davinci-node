package ballotsignature

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	gecc "github.com/consensys/gnark-crypto/ecc"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ethereum"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

func TestSignatureValid(t *testing.T) {
	c := qt.New(t)

	// Test valid signature
	validSig := &Signature{
		R: types.HexBytes{1, 2, 3},
		S: types.HexBytes{4, 5, 6},
	}
	c.Assert(validSig.Valid(), qt.IsTrue)

	// Test invalid signatures
	nilRSig := &Signature{
		R: nil,
		S: types.HexBytes{4, 5, 6},
	}
	c.Assert(nilRSig.Valid(), qt.IsFalse)

	nilSSig := &Signature{
		R: types.HexBytes{1, 2, 3},
		S: nil,
	}
	c.Assert(nilSSig.Valid(), qt.IsFalse)

	nilBothSig := &Signature{
		R: nil,
		S: nil,
	}
	c.Assert(nilBothSig.Valid(), qt.IsFalse)
}

func TestSignatureBigInt(t *testing.T) {
	c := qt.New(t)

	r := types.HexBytes{1, 2, 3}
	s := types.HexBytes{4, 5, 6}
	sig := &Signature{
		R: r,
		S: s,
	}

	rInt, sInt := sig.BigInt()

	c.Assert(rInt.Cmp(new(big.Int).SetBytes(r)), qt.Equals, 0)
	c.Assert(sInt.Cmp(new(big.Int).SetBytes(s)), qt.Equals, 0)
}

func TestSignatureBin(t *testing.T) {
	c := qt.New(t)

	r := types.HexBytes{1, 2, 3}
	s := types.HexBytes{4, 5, 6}
	sig := &Signature{
		R: r,
		S: s,
	}

	bin := sig.Bin()

	// Check that the binary representation is the concatenation of R and S
	expected := append([]byte(r), []byte(s)...)
	c.Assert(bin, qt.DeepEquals, expected)
}

func TestSignEthereumMessage(t *testing.T) {
	c := qt.New(t)

	// Generate private key
	var privKey *ecdsa.PrivateKey
	privKey, err := ethcrypto.GenerateKey()
	c.Assert(err, qt.IsNil)
	c.Assert(privKey, qt.Not(qt.IsNil))

	// Message to sign
	msg := []byte("test message")

	// Sign message
	sig, err := SignEthereumMessage(msg, privKey)
	c.Assert(err, qt.IsNil)
	c.Assert(sig, qt.Not(qt.IsNil))
	c.Assert(sig.Valid(), qt.IsTrue)

	// Get compressed public key
	pubKeyBytes := ethcrypto.CompressPubkey(&privKey.PublicKey)

	// Test the signature directly with go-ethereum's verification
	msgHash := ethereum.Hash(msg)
	ethSignature := sig.Bin()
	// Need to add recovery id for Ethereum signature (V)
	verified := ethcrypto.VerifySignature(pubKeyBytes, msgHash, ethSignature)
	c.Assert(verified, qt.IsTrue)
}

func TestSignatureVerify(t *testing.T) {
	c := qt.New(t)

	// Generate private key
	privKey, err := ethcrypto.GenerateKey()
	c.Assert(err, qt.IsNil)
	c.Assert(privKey, qt.Not(qt.IsNil))

	// Get public key
	pubKeyBytes := ethcrypto.CompressPubkey(&privKey.PublicKey)

	// Create a message as a big integer
	msgBigInt := big.NewInt(12345)

	// Sign the message
	msg := crypto.BigIntToFFwithPadding(msgBigInt, gecc.BLS12_377.ScalarField())
	ethSignature, err := ethcrypto.Sign(ethereum.Hash(msg), privKey)
	c.Assert(err, qt.IsNil)

	// Create a signature from the Ethereum signature
	var sigR, sigS types.HexBytes
	sigR = ethSignature[:32]
	sigS = ethSignature[32:64]
	sig := &Signature{
		R: sigR,
		S: sigS,
	}

	// Verify the signature
	c.Assert(sig.Verify(msgBigInt, pubKeyBytes), qt.IsTrue)

	// Test verification with invalid signature
	invalidSig := &Signature{
		R: types.HexBytes{1, 2, 3},
		S: types.HexBytes{4, 5, 6},
	}
	c.Assert(invalidSig.Verify(msgBigInt, pubKeyBytes), qt.IsFalse)

	// Test with invalid public key
	invalidPubKey := []byte{1, 2, 3, 4, 5}
	c.Assert(sig.Verify(msgBigInt, invalidPubKey), qt.IsFalse)

	// Test with nil signature components
	invalidSig = &Signature{
		R: nil,
		S: sigS,
	}
	c.Assert(invalidSig.Verify(msgBigInt, pubKeyBytes), qt.IsFalse)
}
