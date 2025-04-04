// Package ethereum provides cryptographic operations for Ethereum ECDSA signatures.
package ethereum

import (
	"fmt"
	"math/big"

	gecc "github.com/consensys/gnark-crypto/ecc"
	gecdsa "github.com/consensys/gnark-crypto/ecc/secp256k1/ecdsa"
	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto"
)

const (
	// SignatureLength is the size of an ECDSA signature in bytes
	SignatureLength = ethcrypto.SignatureLength
	// CompressedPubKeyLength is the size of a compressed public key
	CompressedPubKeyLength = 33
	// SignatureMinLength is the minimum length of a signature (without recovery byte)
	SignatureMinLength = ethcrypto.SignatureLength - 1
	// SigningPrefix is the prefix added when hashing Ethereum messages
	SigningPrefix = "\u0019Ethereum Signed Message:\n"
	// HashLength is the size of a keccak256 hash
	HashLength = 32
)

// ECDSASignature represents an Ethereum ECDSA signature with R and S components
type ECDSASignature struct {
	R        *big.Int `json:"r"`
	S        *big.Int `json:"s"`
	recovery byte     `json:"-"`
}

// New creates a new ECDSASignature from raw signature byte payload.
func New(signature []byte) (*ECDSASignature, error) {
	if len(signature) < SignatureMinLength {
		return nil, fmt.Errorf("signature length is less than %d", SignatureMinLength)
	}
	var sig gecdsa.Signature
	if _, err := sig.SetBytes(signature[:64]); err != nil {
		return nil, fmt.Errorf("could not set bytes: %w", err)
	}
	return &ECDSASignature{
		R:        new(big.Int).SetBytes(sig.R[:]),
		S:        new(big.Int).SetBytes(sig.S[:]),
		recovery: signature[64],
	}, nil
}

// Valid method checks if the ECDSASignature is valid. A signature is valid if
// both R and S values are not nil.
func (sig *ECDSASignature) Valid() bool {
	return sig.R != nil && sig.S != nil
}

// Bytes returns the bytes of the binary representation of the signature, which
// is built by appending the R and S values as byte slices and the recovery byte.
func (sig *ECDSASignature) Bytes() []byte {
	rBytes := sig.R.Bytes()
	sBytes := sig.S.Bytes()

	// Ensure R and S are 32 bytes each (pad with leading zeros if necessary)
	r := make([]byte, 32)
	s := make([]byte, 32)
	copy(r[32-len(rBytes):], rBytes)
	copy(s[32-len(sBytes):], sBytes)

	return append(r, append(s, sig.recovery)...)
}

// SetBytes sets the ECDSASignature from a byte slice. The byte slice should be
// at least 65 bytes long, where the first 64 bytes are the R and S values.
func (sig *ECDSASignature) SetBytes(signature []byte) *ECDSASignature {
	if len(signature) < SignatureMinLength {
		return nil
	}
	var sigStruct gecdsa.Signature
	if _, err := sigStruct.SetBytes(signature[:64]); err != nil {
		return nil
	}
	sig.R.SetBytes(sigStruct.R[:])
	sig.S.SetBytes(sigStruct.S[:])
	if len(signature) == SignatureLength {
		sig.recovery = signature[64]
	} else {
		sig.recovery = 0
	}
	return sig
}

// VerifyBLS12377 checks if the signature is valid for the given input and public key.
// The public key should be an ecdsa public key compressed in bytes. The input
// should be a big integer that will be converted in a byte slice ensuring that
// the final value is in the expected scalar field (BLS12_377) and has the
// expected size.
func (sig *ECDSASignature) VerifyBLS12377(signedInput *big.Int, expectedPubKey []byte) bool {
	if !sig.Valid() {
		return false
	}
	ffInput := crypto.BigIntToFFwithPadding(signedInput, gecc.BLS12_377.ScalarField())
	sigBytes := sig.Bytes()
	// Use only the R and S components (first 64 bytes) for verification
	return ethcrypto.VerifySignature(expectedPubKey, HashMessage(ffInput), sigBytes[:64])
}

// Verify checks if the signature is valid for the given input and public key.
func (sig *ECDSASignature) Verify(signedInput []byte, expectedPubKey []byte) bool {
	if !sig.Valid() {
		return false
	}
	sigBytes := sig.Bytes()
	// Use only the R and S components (first 64 bytes) for verification
	return ethcrypto.VerifySignature(expectedPubKey, HashMessage(signedInput), sigBytes[:64])
}

// String returns a string representation of the ECDSASignature, including
func (sig *ECDSASignature) String() string {
	return fmt.Sprintf("R: %s, S: %s, Recovery: %d", sig.R.String(), sig.S.String(), sig.recovery)
}

// AddrFromSignature recovers the Ethereum address that created the signature of a message.
func AddrFromSignature(message, signature []byte) (common.Address, error) {
	if len(signature) < SignatureMinLength {
		return common.Address{}, fmt.Errorf("signature too short (%d)", len(signature))
	}
	// Use recovery ID byte only if signature length is 65 bytes
	if len(signature) == SignatureLength {
		if signature[64] > 1 {
			signature[64] -= 27
		}
		if signature[64] > 1 {
			return common.Address{}, fmt.Errorf("bad recover ID byte")
		}
	}
	pubKey, err := ethcrypto.SigToPub(HashMessage(message), signature)
	if err != nil {
		return common.Address{}, fmt.Errorf("sigToPub %w", err)
	}
	return ethcrypto.PubkeyToAddress(*pubKey), nil
}
