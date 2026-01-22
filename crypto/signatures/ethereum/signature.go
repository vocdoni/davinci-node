// Package ethereum provides cryptographic operations for Ethereum ECDSA signatures.
package ethereum

import (
	"bytes"
	"fmt"
	"math/big"

	gecc "github.com/consensys/gnark-crypto/ecc"
	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/vocdoni/davinci-node/crypto"
	"github.com/vocdoni/davinci-node/types"
)

const (
	// SignatureLength is the size of an ECDSA signature in bytes
	SignatureLength = ethcrypto.SignatureLength
	// CompressedPubKeyLength is the size of a compressed public key
	CompressedPubKeyLength = 33
	// SigningPrefix is the prefix added when hashing Ethereum messages
	SigningPrefix = "\u0019Ethereum Signed Message:\n"
	// HashLength is the size of a keccak256 hash
	HashLength = 32
)

// ECDSASignature represents an Ethereum ECDSA signature with R and S components.
// The components are stored as big.Int values within the secp256k1 curve field.
type ECDSASignature struct {
	R        *big.Int `json:"r"`
	S        *big.Int `json:"s"`
	recovery byte     `json:"-"`
}

// BytesToSignature function creates a new ECDSASignature from raw signature
// byte payload.
func BytesToSignature(signature []byte) (*ECDSASignature, error) {
	if len(signature) < SignatureLength-1 {
		return nil, fmt.Errorf("signature length is less than %d", SignatureLength-1)
	}
	sig := new(ECDSASignature).SetBytes(signature)
	if sig == nil {
		return nil, fmt.Errorf("wrong signature bytes")
	}
	return sig, nil
}

// HexToSignature function decodes the provided hex string to a bytes and the
// decodes the bytes to an ECDSASignature using the BytesToSignature function.
func HexToSignature(hexSignature string) (*ECDSASignature, error) {
	bSignature, err := types.HexStringToHexBytes(hexSignature)
	if err != nil {
		return nil, err
	}
	return BytesToSignature(bSignature)
}

// Valid method checks if the ECDSASignature is valid. A signature is valid if
// both R and S values are not nil.
func (sig *ECDSASignature) Valid() bool {
	return sig.R != nil && sig.S != nil
}

// Bytes returns the bytes of the binary representation of the signature, which
// is built by appending the R and S values as byte slices and the recovery byte.
// The recovery byte is adjusted to the Ethereum standard format (27-30) for compatibility
// with ethcrypto.SigToPub().
func (sig *ECDSASignature) Bytes() []byte {
	rBytes := sig.R.Bytes()
	sBytes := sig.S.Bytes()

	// Ensure R and S are 32 bytes each (pad with leading zeros if necessary)
	r := make([]byte, 32)
	s := make([]byte, 32)
	copy(r[32-len(rBytes):], rBytes)
	copy(s[32-len(sBytes):], sBytes)

	// Subtract 27 from the recovery value to match Ethereum standard
	// Internal representation is 0-3, but Ethereum expects 27-30
	v := sig.recovery
	if v > 1 {
		v -= 27
	}

	return append(r, append(s, v)...)
}

// SetBytes sets the ECDSASignature from a byte slice. The byte slice should be
// at least 65 bytes long, where the first 64 bytes are the R and S values.
func (sig *ECDSASignature) SetBytes(signature []byte) *ECDSASignature {
	if len(signature) < SignatureLength-1 {
		return nil
	}
	//var sigStruct gecdsa.Signature
	//if _, err := sigStruct.SetBytes(signature[:64]); err != nil {
	//	return nil
	//}
	sig.R = new(big.Int).SetBytes(signature[:32])
	sig.S = new(big.Int).SetBytes(signature[32:64])

	if len(signature) == SignatureLength {
		// Make a copy of the recovery byte to avoid modifying the input array
		v := signature[64]
		// Handle Ethereum's "magic" recovery values (27, 28, etc.)
		if v >= 27 {
			v -= 27
		}
		if v > 3 {
			// Invalid recovery byte
			return nil
		}
		sig.recovery = v
	} else {
		sig.recovery = 0
	}

	return sig
}

// VerifyBLS12377 checks if the signature is valid for the given input and
// public key. The public key should be an ecdsa address. The input should be
// a big integer that will be converted in a byte slice ensuring that the final
// value is in the expected scalar field (BLS12_377) and has the expected size.
func (sig *ECDSASignature) VerifyBLS12377(signedInput *big.Int, expectedAddress common.Address) (bool, []byte) {
	ffInput := crypto.BigIntToFFToSign(signedInput, gecc.BLS12_377.ScalarField())
	return sig.Verify(ffInput, expectedAddress)
}

// VerifyVoteID checks if the signature is valid for the given voteID.
// This method also checks that the public key matches the passed expectedAddress
func (sig *ECDSASignature) VerifyVoteID(voteID types.VoteID, expectedAddress common.Address) (bool, []byte) {
	return sig.Verify(crypto.PadToSign(voteID.Bytes()), expectedAddress)
}

// Verify checks that `sig` is a valid signature of `signedInput` produced by `expectedAddress`,
// by recovering the public key from (signedInput, sig) and comparing its derived address.
// It returns the recovered public key.
func (sig *ECDSASignature) Verify(signedInput []byte, expectedAddress common.Address) (bool, []byte) {
	if !sig.Valid() {
		return false, nil
	}
	pubKey, err := ethcrypto.SigToPub(HashMessage(signedInput), sig.Bytes())
	if err != nil {
		return false, nil
	}
	// Check if the public key matches the expected address
	return bytes.Equal(ethcrypto.PubkeyToAddress(*pubKey).Bytes(), expectedAddress.Bytes()), ethcrypto.FromECDSAPub(pubKey)
}

// String returns a string representation of the ECDSASignature, including
func (sig *ECDSASignature) String() string {
	return fmt.Sprintf("R: %s, S: %s, Recovery: %d", sig.R.String(), sig.S.String(), sig.recovery)
}

// AddrFromSignature recovers the Ethereum address that created the signature of a message.
func AddrFromSignature(message []byte, signature *ECDSASignature) (common.Address, error) {
	if signature == nil || !signature.Valid() {
		return common.Address{}, fmt.Errorf("signature is nil")
	}
	pubKey, err := ethcrypto.SigToPub(HashMessage(message), signature.Bytes())
	if err != nil {
		return common.Address{}, fmt.Errorf("sigToPub %w", err)
	}
	return ethcrypto.PubkeyToAddress(*pubKey), nil
}
