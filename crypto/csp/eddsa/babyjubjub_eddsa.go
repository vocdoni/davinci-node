package eddsa

import (
	"fmt"
	"hash"
	"math/big"

	bn254 "github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/ethereum/go-ethereum/common"
	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/iden3/go-iden3-crypto/poseidon"
	"github.com/vocdoni/davinci-node/types"
)

// DefaultHashFn is the default hash function used by the BabyJubJubEdDSA
var DefaultHashFn hash.Hash

func init() {
	var err error
	// Initialize the default hash function as Poseidon
	if DefaultHashFn, err = poseidon.New(6); err != nil {
		panic(err)
	}
}

// BabyJubJubEdDSA struct implements the CSP interface for the
// BabyJubJubEdDSA over multiple curves.
type BabyJubJubEdDSA struct {
	hashFn  hash.Hash
	privKey babyjub.PrivateKey
}

// New creates a new New for the bn254 curve using Poseidon as hash function. It implements the CSP interface and can be
// used to generate and verify proofs for voters. It generates a new random
// private key. If something goes wrong during the key generation, it returns
// an error.
func New(hashFn hash.Hash) (*BabyJubJubEdDSA, error) {
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
	// Compute the hash of the seed
	if _, err := c.hashFn.Write(seed); err != nil {
		return fmt.Errorf("error hashing seed: %w", err)
	}
	seedBytes := c.hashFn.Sum(nil)
	c.hashFn.Reset()
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

// CensusRoot returns the census root computed from the public key of the
// EdDSA instance. It uses the X and Y coordinates of the public key's point
// to compute the hash. If the EdDSA signer is not initialized or the public
// key can not be converted to censusRoot for the instance curve, it returns
// nil.
func (c *BabyJubJubEdDSA) CensusRoot() *types.CensusRoot {
	// Convert the public key into a census root
	censusRoot, err := pubKeyPointToCensusRoot(c.privKey.Public())
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
	message, err := signatureMessage(processID, address.Bytes(), weight)
	if err != nil {
		return nil, fmt.Errorf("error composing signature message: %w", err)
	}
	// Compute the signature of the message and encode it into bytes
	signature := c.privKey.SignPoseidon(message.BigInt().MathBigInt())
	encSignature, err := signature.Compress().MarshalText()
	if err != nil {
		return nil, fmt.Errorf("error marshaling signature: %w", err)
	}
	// Convert the public key to a census root
	censusRoot, err := pubKeyPointToCensusRoot(c.privKey.Public())
	if err != nil {
		return nil, fmt.Errorf("error computing census root: %w", err)
	}
	// Encode the public key into bytes
	encPublicKey, err := c.privKey.Public().MarshalText()
	if err != nil {
		return nil, fmt.Errorf("error marshaling public key: %w", err)
	}
	return &types.CensusProof{
		CensusOrigin: c.CensusOrigin(),
		Root:         censusRoot,
		Address:      address.Bytes(),
		Weight:       weight,
		ProcessID:    processID,
		PublicKey:    encPublicKey,
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
	pubKey, err := pubKeyFromCensusProof(proof)
	if err != nil {
		return fmt.Errorf("error getting public key from census proof: %w", err)
	}
	// Recompute the signature message
	message, err := signatureMessage(proof.ProcessID, proof.Address, proof.Weight)
	if err != nil {
		return fmt.Errorf("error composing signature message: %w", err)
	}
	// Decode the signature bytes
	signature, err := babyjub.DecompressSig(proof.Signature)
	if err != nil {
		return fmt.Errorf("error decompressing signature: %w", err)
	}
	// Verify the signature using the public key and the message
	if verified := pubKey.VerifyPoseidon(message.BigInt().MathBigInt(), signature); !verified {
		return fmt.Errorf("signature verification failed for address %s", proof.Address.String())
	}
	return nil
}

// pubKeyFromCensusProof function returns a decoded public key from the proof
// provided acording to its census origin.
func pubKeyFromCensusProof(proof *types.CensusProof) (*babyjub.PublicKey, error) {
	switch proof.CensusOrigin {
	case types.CensusOriginCSPEdDSABabyJubJubV1:
		// Decode the public key into bytes
		pubKey := &babyjub.PublicKey{}
		if err := pubKey.UnmarshalText(proof.PublicKey); err != nil {
			return nil, fmt.Errorf("error unmarshaling public key: %w", err)
		}
		return pubKey, nil
	default:
		return nil, fmt.Errorf("unsupported census origin: %d", proof.CensusOrigin)
	}
}

// pubKeyPointToCensusRoot function encodes the public key provided as a census
// root by hashing its coords accordingly to the curve provided. It uses the
// poseidon hash function for that curve to calculate the hash of the public
// key coords. It returns an error if the public key provided is invalid for
// the desired curve or if the curve is not supported.
func pubKeyPointToCensusRoot(
	publicKey *babyjub.PublicKey,
) (types.HexBytes, error) {
	// Hash the public key using the poseidon hash function
	hashedPubKey, err := poseidon.Hash([]*big.Int{publicKey.X, publicKey.Y})
	if err != nil {
		return nil, fmt.Errorf("error hashing public key: %w", err)
	}
	return hashedPubKey.Bytes(), nil
}

// signatureMessage composes the message to be signed by the CSP. The message
// is the concatenation of the process ID and address, both converted to field
// elements suitable for the circuit.
func signatureMessage(processID types.ProcessID, address types.HexBytes, weight *types.BigInt) (types.HexBytes, error) {
	// Inputs checks
	if !processID.IsValid() {
		return nil, fmt.Errorf("invalid process ID")
	}
	if len(address) == 0 {
		return nil, fmt.Errorf("address must not be empty")
	}
	// Hash the process ID and address to create a message suitable for signing
	// using the poseidon hash function. Ensure that the process ID and address
	// are converted to field elements for the curve.
	res, err := poseidon.Hash([]*big.Int{
		processID.BigInt().ToFF(bn254.ID.ScalarField()).MathBigInt(),
		address.BigInt().ToFF(bn254.ID.ScalarField()).MathBigInt(),
		weight.ToFF(bn254.ID.ScalarField()).MathBigInt(),
	})
	if err != nil {
		return nil, fmt.Errorf("error hashing signature message: %w", err)
	}
	return res.Bytes(), nil
}
