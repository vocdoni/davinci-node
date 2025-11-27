package ethereum

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	gecdsa "github.com/consensys/gnark-crypto/ecc/secp256k1/ecdsa"
	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/vocdoni/davinci-node/types"
)

// Signer represents an ECDSA private key for signing Ethereum messages. It is
// a wrapper around the go-ethereum ecdsa.PrivateKey type. The signature is
// performed by hashing (keccak256) the message with a prefix (Ethereum Signed
// Message) and then signing the hash with the private key.
type Signer ecdsa.PrivateKey

// Address returns the Ethereum address derived from the public key of the signer.
func (s *Signer) Address() common.Address {
	return ethcrypto.PubkeyToAddress(s.PublicKey)
}

// HexPrivateKey returns the hex-encoded representation of the ECDSA private
// key.
func (s *Signer) HexPrivateKey() types.HexBytes {
	return types.HexBytes(ethcrypto.FromECDSA((*ecdsa.PrivateKey)(s)))
}

// Sign signs a message using the ECDSA private key and returns the signature.
// The message is hashed with the Ethereum prefix before signing.
func (s *Signer) Sign(msg []byte) (*ECDSASignature, error) {
	return Sign(msg, (*ecdsa.PrivateKey)(s))
}

// NewSigner creates a new ECDSA private key for signing.
func NewSigner() (*Signer, error) {
	s, err := ethcrypto.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("could not generate key: %w", err)
	}
	return (*Signer)(s), nil
}

// NewSignerFromHex creates a new ECDSA private key from a hex-encoded string.
func NewSignerFromHex(hexKey string) (*Signer, error) {
	s, err := ethcrypto.HexToECDSA(hexKey)
	if err != nil {
		return nil, fmt.Errorf("could not generate key: %w", err)
	}
	return (*Signer)(s), nil
}

// NewSignerFromSeed creates a new ECDSA private key from a seed, no matter the
// length of the seed. It calculates the hash of the seed to use the right length.
func NewSignerFromSeed(seed []byte) (*Signer, error) {
	h := ethcrypto.Keccak256(seed)
	s, err := ethcrypto.ToECDSA(h)
	if err != nil {
		return nil, fmt.Errorf("could not generate key: %w", err)
	}
	return (*Signer)(s), nil
}

// Sign signs an Ethereum message (adding the corresponding prefix) using the
// given private key.
func Sign(msg []byte, privKey *ecdsa.PrivateKey) (*ECDSASignature, error) {
	ethSignature, err := ethcrypto.Sign(HashMessage(msg), privKey)
	if err != nil {
		return nil, fmt.Errorf("could not sign message: %w", err)
	}
	var sig gecdsa.Signature
	if _, err := sig.SetBytes(ethSignature[:64]); err != nil {
		return nil, fmt.Errorf("could not set bytes: %w", err)
	}

	return &ECDSASignature{
		R:        new(big.Int).SetBytes(sig.R[:]),
		S:        new(big.Int).SetBytes(sig.S[:]),
		recovery: ethSignature[64],
	}, nil
}

// HashMessage performs a keccak256 hash over the data adding Ethereum Message
// prefix.
func HashMessage(data []byte) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s%d%s", SigningPrefix, len(data), data)
	return HashRaw(buf.Bytes())
}

// HashRaw hashes data with no prefix using Keccak256.
func HashRaw(data []byte) []byte {
	return ethcrypto.Keccak256(data)
}
