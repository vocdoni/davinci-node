package eddsa

import (
	"fmt"
	"math/big"

	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/vocdoni/davinci-node/types"
)

// CompressedBytes represents the compressed bytes resulting of babyjubjub
// compress and marshal texts from public keys and signatures.
type CompressedBytes []byte

// SetCompressed method sets the resulting bytes of MarshalText and Compress
// functions from public keys and signatures of Iden3 Babyjubjub crypto. These
// bytes are slices of bytes of big numbers decimal strings. This method
// convert that bytes into a real hex bytes.
func (c CompressedBytes) SetCompressed(b types.HexBytes) (CompressedBytes, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("bytes provided are empty")
	}
	biBytes, ok := new(big.Int).SetString(b.Hex(), 10)
	if !ok {
		return nil, fmt.Errorf("error converting bytes to big int")
	}
	copy(c, biBytes.Bytes())
	return biBytes.Bytes(), nil
}

// SetBytes method sets the resulting bytes of MarshalText and Compress
// functions from public keys and signatures of Iden3 Babyjubjub crypto.
func (c CompressedBytes) SetBytes(b types.HexBytes) (CompressedBytes, error) {
	copy(c, b)
	return b.Bytes(), nil
}

// Bytes method returns the underlying byte slice of the CompressedBytes.
func (c CompressedBytes) Bytes() types.HexBytes {
	return types.HexBytes(c)
}

// Decompress method returns the decompressed bytes from compressed bytes. It
// revert the process of Compress method, by converting the compressed bytes
// into a big int string and then into a slice of bytes.
func (c CompressedBytes) Decompress() (types.HexBytes, error) {
	if len(c) == 0 {
		return nil, fmt.Errorf("bytes provided are empty")
	}
	// Convert the hex bytes into a big int string
	encodedHexText := new(big.Int).SetBytes(c.Bytes()).String()
	// Decode the big int string into a bytes
	decodedBytes, err := types.HexStringToHexBytes(encodedHexText)
	if err != nil {
		return nil, fmt.Errorf("error decoding bytes text: %w", err)
	}
	return decodedBytes, nil
}

// CompressSignature method returns the compressed bytes from a signature.
func CompressSignature(sig *babyjub.Signature) (CompressedBytes, error) {
	compressedBytes, err := sig.Compress().MarshalText()
	if err != nil {
		return nil, fmt.Errorf("error compressing signature: %w", err)
	}
	return new(CompressedBytes).SetCompressed(compressedBytes)
}

// DecompressSignature method returns the decompressed bytes from a Iden3
// babyjubjub signature.
func DecompressSignature(signature types.HexBytes) (*babyjub.Signature, error) {
	compressed, err := new(CompressedBytes).SetBytes(signature)
	if err != nil {
		return nil, fmt.Errorf("error setting compressed bytes: %w", err)
	}
	decompressed, err := compressed.Decompress()
	if err != nil {
		return nil, fmt.Errorf("error decompressing signature: %w", err)
	}
	sig, err := babyjub.DecompressSig(decompressed)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling signature: %w", err)
	}
	return sig, nil
}

// CompressedPublicKey method returns the compressed bytes from Iden3
// babyjubjub public key.
func CompressedPublicKey(pubKey *babyjub.PublicKey) (CompressedBytes, error) {
	// Encode the public key into bytes using the babyjubjub format, which
	// results in a big int string into []bytes.
	compressedBytes, err := pubKey.MarshalText()
	if err != nil {
		return nil, fmt.Errorf("error compressing public key: %w", err)
	}
	// Use CompressedBytes to convert the string bigint []bytes into a real
	// hex bytes.
	return new(CompressedBytes).SetCompressed(compressedBytes)
}

// DecompressPublicKey method returns the decompressed bytes from a Iden3
// babyjubjub public key.
func DecompressPublicKey(pubKey types.HexBytes) (*babyjub.PublicKey, error) {
	compressed, err := new(CompressedBytes).SetBytes(pubKey)
	if err != nil {
		return nil, fmt.Errorf("error setting compressed bytes: %w", err)
	}
	decompressed, err := compressed.Decompress()
	if err != nil {
		return nil, fmt.Errorf("error decompressing public key: %w", err)
	}
	pk := &babyjub.PublicKey{}
	if err := pk.UnmarshalText(decompressed); err != nil {
		return nil, fmt.Errorf("error unmarshaling public key: %w", err)
	}
	return pk, nil
}
