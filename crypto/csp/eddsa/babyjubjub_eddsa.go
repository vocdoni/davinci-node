package eddsa

import (
	"fmt"
	"math/big"

	bn254 "github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/ethereum/go-ethereum/common"
	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/vocdoni/davinci-node/types"
)

// BabyJubJubEdDSA struct implements the CSP interface for the
// BabyJubJubEdDSA over multiple curves.
type BabyJubJubEdDSA struct {
	hashFn  Hash
	privKey babyjub.PrivateKey
}

// NewBabyJubJubKeyFromSeed creates a new BabyJubJubEdDSA for the bn254 curve
// using the hash function provided and the provided seed. It implements the
// CSP interface and can be used to generate and verify proofs for voters. If
// something goes wrong during the key generation, it returns an error.
func NewBabyJubJubKeyFromSeed(hashFn Hash, seed []byte) (*BabyJubJubEdDSA, error) {
	key := &BabyJubJubEdDSA{
		hashFn: hashFn,
	}
	if err := key.SetSeed(seed); err != nil {
		return nil, err
	}
	return key, nil
}

// NewBabyJubJubKeyRandom creates a new random BabyJubJubEdDSA for the bn254
// curve using the hash function provided. It implements the CSP interface and
// can be used to generate and verify proofs for voters. If something goes
// wrong during the key generation, it returns an error.
func NewBabyJubJubKeyRandom(hashFn Hash) (*BabyJubJubEdDSA, error) {
	randPrivKey := babyjub.NewRandPrivKey()
	return &BabyJubJubEdDSA{
		hashFn:  hashFn,
		privKey: randPrivKey,
	}, nil
}

// SetSeed sets the seed for the EdDSA instance. It generates a new private
// key using the provided seed. The seed must not be empty, and it is used
// to derive the private key for the curve of the EdDSA instance. If the seed
// is empty or if there is an error during key generation, it returns an error.
func (c *BabyJubJubEdDSA) SetSeed(seed []byte) error {
	// Ensure seed is not empty
	if len(seed) == 0 {
		return fmt.Errorf("seed cannot be empty")
	}
	// Reset the hash function before using it
	c.hashFn.Reset()
	// Compute the hash of the seed
	if _, err := c.hashFn.Write(seed); err != nil {
		return fmt.Errorf("error hashing seed: %w", err)
	}
	seedBytes := c.hashFn.Sum(nil)
	// Convert seed to [32]byte
	var rawPrivKey [32]byte
	copy(rawPrivKey[:], seedBytes)
	c.privKey = babyjub.PrivateKey(rawPrivKey)
	return nil
}

// CensusOrigin returns the origin of the credential service providers. It
// returns the type of the CSP, which is EdDSA in this case.
func (c *BabyJubJubEdDSA) CensusOrigin() types.CensusOrigin {
	return types.CensusOriginCSPEdDSABabyJubJubV1
}

// PublicKey returns the public key of the EdDSA instance. It returns the
// public key as a hex bytes.
func (c *BabyJubJubEdDSA) PublicKey() (types.HexBytes, error) {
	// Encode the public key into bytes using the babyjubjub format, which
	// results in a big int string into []bytes.
	marshaledPubKey, err := c.privKey.Public().MarshalText()
	if err != nil {
		return nil, fmt.Errorf("error marshaling public key: %w", err)
	}
	// Use EncodeBigIntBytes to convert the string bigint []bytes into a real
	// hex bytes.
	return DecimalStringBytesToHexBytes(marshaledPubKey)
}

// CensusRoot returns the census root computed from the public key of the
// EdDSA instance. It uses the X and Y coordinates of the public key's point
// to compute the hash. If the EdDSA signer is not initialized or the public
// key can not be converted to censusRoot for the instance curve, it returns
// nil.
func (c *BabyJubJubEdDSA) CensusRoot() *types.CensusRoot {
	// Convert the public key into a census root
	censusRoot, err := pubKeyPointToCensusRoot(c.hashFn, c.privKey.Public())
	if err != nil {
		return nil
	}
	// Return it as a normalized census root
	return &types.CensusRoot{
		Root: types.NormalizedCensusRoot(censusRoot),
	}
}

// GenerateProof generates a census proof for the given process ID and
// address. It signs the message composed by the process ID and address using
// the private key of the CredentialServiceProviders. It returns a CensusProof
// struct that includes the hash of the public key as the root, the public
// key, the signature and the signed address and process ID. It returns an
// error if the EdDSA signer is not initialized, the process ID or address
// provided are not valid, or something fails during signature process.
func (c *BabyJubJubEdDSA) GenerateProof(
	processID types.ProcessID,
	address common.Address,
	weight *types.BigInt,
) (*types.CensusProof, error) {
	// Inputs validation
	if !processID.IsValid() {
		return nil, fmt.Errorf("invalid process ID")
	}
	if address == (common.Address{}) {
		return nil, fmt.Errorf("invalid address")
	}
	if weight == nil || weight.LessThanOrEqual(new(types.BigInt).SetInt(0)) {
		return nil, fmt.Errorf("invalid weight")
	}
	// Sign the message composed by the process ID and address using the hash
	// function and the private key of the CSP
	message, err := signatureMessage(c.hashFn, processID, address.Bytes(), weight)
	if err != nil {
		return nil, fmt.Errorf("error composing signature message: %w", err)
	}
	// Compute the signature of the message
	signature := c.privKey.SignPoseidon(message.BigInt().MathBigInt())
	// Compress the signature to a bigint string bytes
	bSignature, err := signature.Compress().MarshalText()
	if err != nil {
		return nil, fmt.Errorf("error marshaling signature: %w", err)
	}
	// Encode the signature into real hex bytes
	encSignature, err := DecimalStringBytesToHexBytes(bSignature)
	if err != nil {
		return nil, fmt.Errorf("error encoding signature: %w", err)
	}
	// Convert the public key to a census root
	censusRoot, err := pubKeyPointToCensusRoot(c.hashFn, c.privKey.Public())
	if err != nil {
		return nil, fmt.Errorf("error computing census root: %w", err)
	}
	// Get the public key in hex bytes
	publicKey, err := c.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("error marshaling public key: %w", err)
	}
	return &types.CensusProof{
		CensusOrigin: c.CensusOrigin(),
		Root:         censusRoot,
		Address:      address.Bytes(),
		Weight:       weight,
		ProcessID:    processID,
		PublicKey:    publicKey,
		Signature:    encSignature,
	}, nil
}

// VerifyProof method verifies the proof provided for the current EdDSA curve.
// It also verifies that the census origin of the proof matches with the EdDSA
// instance one. It returns an error if the proof provided is nil, the census
// origins do not match or something fails during signature verification
// process.
func (c *BabyJubJubEdDSA) VerifyProof(proof *types.CensusProof) error {
	// Proof inputs checks
	if proof == nil {
		return fmt.Errorf("proof is nil")
	}
	if !proof.ProcessID.IsValid() {
		return fmt.Errorf("process ID is nil")
	}
	if proof.CensusOrigin != c.CensusOrigin() {
		return fmt.Errorf("proof origin mismatch: expected %s, got %s", c.CensusOrigin(), proof.CensusOrigin)
	}
	// Get the public key from the proof
	pubKey, err := PublicKeyFromBytes(proof.PublicKey)
	if err != nil {
		return fmt.Errorf("error getting public key from census proof: %w", err)
	}
	// Recompute the signature message
	message, err := signatureMessage(c.hashFn, proof.ProcessID, proof.Address, proof.Weight)
	if err != nil {
		return fmt.Errorf("error composing signature message: %w", err)
	}
	// Decode the signature from hex bytes to a bigint string bytes
	decSignature, err := HexBytesToDecimalStringBytes(proof.Signature)
	if err != nil {
		return fmt.Errorf("error decoding signature: %w", err)
	}
	// Decompress the signature
	signature, err := babyjub.DecompressSig(decSignature)
	if err != nil {
		return fmt.Errorf("error decompressing signature: %w", err)
	}
	// Verify the signature using the public key and the message
	if verified := pubKey.VerifyPoseidon(message.BigInt().MathBigInt(), signature); !verified {
		return fmt.Errorf("signature verification failed for address %s", proof.Address.String())
	}
	return nil
}

// pubKeyPointToCensusRoot function encodes the public key provided as a census
// root by hashing its coords accordingly to the curve provided. It uses the
// poseidon hash function for that curve to calculate the hash of the public
// key coords. It returns an error if the public key provided is invalid for
// the desired curve or if the curve is not supported.
func pubKeyPointToCensusRoot(
	hashFn Hash,
	publicKey *babyjub.PublicKey,
) (types.HexBytes, error) {
	// Reset the hash function before using it
	hashFn.Reset()
	// Hash the public key using the poseidon hash function
	hashedPubKey, err := hashFn.BigIntsSum([]*big.Int{publicKey.X, publicKey.Y})
	if err != nil {
		return nil, fmt.Errorf("error hashing public key: %w", err)
	}
	return hashedPubKey.Bytes(), nil
}

// signatureMessage composes the message to be signed by the CSP. The message
// is the concatenation of the process ID and address, both converted to field
// elements suitable for the circuit.
func signatureMessage(
	hashFn Hash,
	processID types.ProcessID,
	address types.HexBytes,
	weight *types.BigInt,
) (types.HexBytes, error) {
	// Inputs checks
	if !processID.IsValid() {
		return nil, fmt.Errorf("invalid process ID")
	}
	if len(address) == 0 || !address.BigInt().IsInField(bn254.ID.ScalarField()) {
		return nil, fmt.Errorf("address must not be empty and must be in field %s", bn254.ID.ScalarField().String())
	}
	if weight == nil || !weight.IsInField(bn254.ID.ScalarField()) {
		return nil, fmt.Errorf("weight must not be nil and must be in field %s", bn254.ID.ScalarField().String())
	}
	// Reset the hash function before using it
	hashFn.Reset()
	// Hash the process ID and address to create a message suitable for signing
	// using the poseidon hash function. Ensure that the process ID and address
	// are converted to field elements for the curve.
	res, err := hashFn.BigIntsSum([]*big.Int{
		processID.BigInt().MathBigInt(),
		address.BigInt().MathBigInt(),
		weight.MathBigInt(),
	})
	if err != nil {
		return nil, fmt.Errorf("error hashing signature message: %w", err)
	}
	return res.Bytes(), nil
}

// PublicKeyFromBytes function decodes the public key hex bytes into a babyjub
// public key format by decoding the hex bytes into a big int string bytes and
// then unmarshaling the big int string bytes into a babyjub public key.
func PublicKeyFromBytes(publicKey types.HexBytes) (*babyjub.PublicKey, error) {
	// Decode the public key from hex bytes to a big int string bytes
	decodedBytes, err := HexBytesToDecimalStringBytes(publicKey)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	// Unmarshal the big int string bytes into a babyjub public key
	pubKey := &babyjub.PublicKey{}
	if err := pubKey.UnmarshalText(decodedBytes); err != nil {
		return nil, fmt.Errorf("unmarshal public key: %w", err)
	}
	return pubKey, nil
}

// DecimalStringBytesToHexBytes function encodes a bigint string bytes into a real hex
// bytes.
func DecimalStringBytesToHexBytes(biStrBytes types.HexBytes) (types.HexBytes, error) {
	if len(biStrBytes) == 0 {
		return nil, fmt.Errorf("bytes provided are empty")
	}
	biPubKey, ok := new(big.Int).SetString(biStrBytes.Hex(), 10)
	if !ok {
		return nil, fmt.Errorf("error converting bytes to big int")
	}
	return biPubKey.Bytes(), nil
}

// HexBytesToDecimalStringBytes function decodes a real hex bytes into a bigint
// string bytes.
func HexBytesToDecimalStringBytes(hexBytes types.HexBytes) (types.HexBytes, error) {
	if len(hexBytes) == 0 {
		return nil, fmt.Errorf("bytes provided are empty")
	}
	// Convert the hex bytes into a big int string
	encodedHexText := new(big.Int).SetBytes(hexBytes).String()
	// Decode the big int string into a bytes
	decodedBytes, err := types.HexStringToHexBytes(encodedHexText)
	if err != nil {
		return nil, fmt.Errorf("error decoding bytes text: %w", err)
	}
	return decodedBytes, nil
}
