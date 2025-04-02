// Package ethereum provides cryptographic operations for Ethereum.
package ethereum

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"github.com/vocdoni/vocdoni-z-sandbox/util"
)

const (
	// SignatureLength is the size of an ECDSA signature in hexString format
	SignatureLength = ethcrypto.SignatureLength
	// PubKeyLengthBytes is the size of a Public Key
	PubKeyLengthBytes = 33
	// PubKeyLengthBytesUncompressed is the size of a uncompressed Public Key
	PubKeyLengthBytesUncompressed = 65
	// SigningPrefix is the prefix added when hashing
	SigningPrefix = "\u0019Ethereum Signed Message:\n"
	// HashLength is the size of a keccak256 hash
	HashLength = 32
)

// SignKeys represents an ECDSA pair of keys for signing.
// Authorized addresses is a list of Ethereum like addresses which are checked on Verify
type SignKeys struct {
	Public  ecdsa.PublicKey
	Private *ecdsa.PrivateKey
}

// NewSignKeys creates a new ECDSA signer. If privkey is nil, a new key pair is generated
func NewSignKeys(privkey *ecdsa.PrivateKey) *SignKeys {
	signer := &SignKeys{
		Private: privkey,
	}
	if signer.Private == nil {
		if err := signer.Generate(); err != nil {
			panic(err)
		}
	} else {
		signer.Public = signer.Private.PublicKey
	}
	return signer
}

// NewSignKeysBatch creates a set of eth random signing keys.
func NewSignKeysBatch(n int) []*SignKeys {
	s := make([]*SignKeys, n)
	for i := 0; i < n; i++ {
		s[i] = NewSignKeys(nil)
		if err := s[i].Generate(); err != nil {
			panic(err)
		}
	}
	return s
}

// Generate generates new keys
func (k *SignKeys) Generate() error {
	key, err := ethcrypto.GenerateKey()
	if err != nil {
		return err
	}
	k.Private = key
	k.Public = key.PublicKey
	return nil
}

// SetHexKey imports a private hex key.
func (k *SignKeys) SetHexKey(privHex string) error {
	key, err := ethcrypto.HexToECDSA(util.TrimHex(privHex))
	if err != nil {
		return err
	}
	k.Private = key
	k.Public = key.PublicKey
	return nil
}

// HexString returns the public compressed and private keys as hex strings
func (k *SignKeys) HexString() (string, string) {
	pubHexComp := fmt.Sprintf("%x", ethcrypto.CompressPubkey(&k.Public))
	privHex := fmt.Sprintf("%x", ethcrypto.FromECDSA(k.Private))
	return pubHexComp, privHex
}

// PublicKey returns the compressed public key as hex bytes.
func (k *SignKeys) PublicKey() types.HexBytes {
	return ethcrypto.CompressPubkey(&k.Public)
}

// PrivateKey returns the private key
func (k *SignKeys) PrivateKey() *ecdsa.PrivateKey {
	return k.Private
}

// DecompressPubKey takes a compressed public key and returns it descompressed. If already decompressed, returns the same key.
func DecompressPubKey(pubComp types.HexBytes) (types.HexBytes, error) {
	if len(pubComp) > PubKeyLengthBytes {
		return pubComp, nil
	}
	pub, err := ethcrypto.DecompressPubkey(pubComp)
	if err != nil {
		return nil, fmt.Errorf("decompress pubKey %w", err)
	}
	return ethcrypto.FromECDSAPub(pub), nil
}

// CompressPubKey returns the compressed public key in hexString format
func CompressPubKey(pubHexDec string) (string, error) {
	pubHexDec = util.TrimHex(pubHexDec)
	if len(pubHexDec) < PubKeyLengthBytesUncompressed*2 {
		return pubHexDec, nil
	}
	pubBytes, err := hex.DecodeString(pubHexDec)
	if err != nil {
		return "", err
	}
	pub, err := ethcrypto.UnmarshalPubkey(pubBytes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", ethcrypto.CompressPubkey(pub)), nil
}

// Address returns the SignKeys ethereum address
func (k *SignKeys) Address() ethcommon.Address {
	return ethcrypto.PubkeyToAddress(k.Public)
}

// AddressString returns the ethereum Address as string
func (k *SignKeys) AddressString() string {
	return ethcrypto.PubkeyToAddress(k.Public).String()
}

// SignEthereum signs a message. Message is a normal string (no HexString nor a Hash)
func (k *SignKeys) SignEthereum(message []byte) ([]byte, error) {
	if k.Private.D == nil {
		return nil, errors.New("no private key available")
	}
	signature, err := ethcrypto.Sign(Hash(message), k.Private)
	if err != nil {
		return nil, err
	}
	return signature, nil
}

// AddrFromPublicKey standaolone function to obtain the Ethereum address from a ECDSA public key
func AddrFromPublicKey(pub []byte) (ethcommon.Address, error) {
	var err error
	if len(pub) <= PubKeyLengthBytes {
		pub, err = DecompressPubKey(pub)
		if err != nil {
			return ethcommon.Address{}, err
		}
	}
	pubkey, err := ethcrypto.UnmarshalPubkey(pub)
	if err != nil {
		return ethcommon.Address{}, err
	}
	return ethcrypto.PubkeyToAddress(*pubkey), nil
}

// AddrFromBytes returns the Ethereum address from a byte array
func AddrFromBytes(addr []byte) ethcommon.Address {
	return ethcommon.BytesToAddress(addr)
}

// PubKeyFromPrivateKey returns the hex public key given a hex private key
func PubKeyFromPrivateKey(privHex string) (string, error) {
	s := NewSignKeys(nil)
	if err := s.SetHexKey(privHex); err != nil {
		return "", err
	}
	pub, _ := s.HexString()
	return pub, nil
}

// PubKeyFromSignature recovers the ECDSA public key that created the signature of a message
// public key is hex encoded.
func PubKeyFromSignature(message, signature []byte) ([]byte, error) {
	if len(signature) != SignatureLength {
		return nil, fmt.Errorf("signature length not correct (%d)", len(signature))
	}
	if signature[64] > 1 {
		signature[64] -= 27
	}
	if signature[64] > 1 {
		return nil, errors.New("bad recover ID byte")
	}
	pubKey, err := ethcrypto.SigToPub(Hash(message), signature)
	if err != nil {
		return nil, fmt.Errorf("sigToPub %w", err)
	}
	// Temporary until the client side changes to compressed keys
	return ethcrypto.CompressPubkey(pubKey), nil
}

// AddrFromSignature recovers the Ethereum address that created the signature of a message.
func AddrFromSignature(message, signature []byte) (ethcommon.Address, error) {
	pub, err := PubKeyFromSignature(message, signature)
	if err != nil {
		return ethcommon.Address{}, err
	}
	return AddrFromPublicKey(pub)
}

// Hash data adding Ethereum Message prefix.
func Hash(data []byte) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s%d%s", SigningPrefix, len(data), data)
	return HashRaw(buf.Bytes())
}

// HashRaw hashes data with no prefix.
func HashRaw(data []byte) []byte {
	return ethcrypto.Keccak256(data)
}
