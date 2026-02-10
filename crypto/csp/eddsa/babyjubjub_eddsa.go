package eddsa

import (
	"fmt"
	"math/big"

	bn254 "github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/ethereum/go-ethereum/common"
	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

// BabyJubJubEdDSA struct implements the CSP interface for the
// BabyJubJubEdDSA over multiple curves.
type BabyJubJubEdDSA struct {
	hashFn  Hash
	privKey babyjub.PrivateKey
	indexFn types.CSPIndexFn
}

// NewBabyJubJubKeyFromSeed creates a new BabyJubJubEdDSA for the bn254 curve
// using the hash function provided and the provided seed. It implements the
// CSP interface and can be used to generate and verify proofs for voters. If
// something goes wrong during the key generation, it returns an error.
func NewBabyJubJubKeyFromSeed(hashFn Hash, seed []byte) (*BabyJubJubEdDSA, error) {
	// Ensure seed is not empty
	if len(seed) == 0 {
		return nil, fmt.Errorf("seed cannot be empty")
	}
	// Reset the hash function before using it
	hashFn.Reset()
	// Compute the hash of the seed
	if _, err := hashFn.Write(seed); err != nil {
		return nil, fmt.Errorf("error hashing seed: %w", err)
	}
	seedBytes := hashFn.Sum(nil)
	// Convert seed to [32]byte
	var rawPrivKey [32]byte
	copy(rawPrivKey[:], seedBytes)
	return &BabyJubJubEdDSA{
		hashFn:  hashFn,
		privKey: babyjub.PrivateKey(rawPrivKey),
		indexFn: DefaultCSPIndexFn,
	}, nil
}

// NewBabyJubJubKey creates a new random BabyJubJubEdDSA for the bn254 curve
// using the hash function provided. It implements the CSP interface and can
// be used to generate and verify proofs for voters. If something goes wrong
// during the key generation, it returns an error.
func NewBabyJubJubKey(hashFn Hash) (*BabyJubJubEdDSA, error) {
	randPrivKey := babyjub.NewRandPrivKey()
	return &BabyJubJubEdDSA{
		hashFn:  hashFn,
		privKey: randPrivKey,
		indexFn: DefaultCSPIndexFn,
	}, nil
}

// SetIndexFn sets the index function for the BabyJubJubEdDSA instance.
func (c *BabyJubJubEdDSA) SetIndexFn(indexFn types.CSPIndexFn) {
	c.indexFn = indexFn
}

// CensusOrigin returns the origin of the credential service providers. It
// returns the type of the CSP, which is EdDSA in this case.
func (c *BabyJubJubEdDSA) CensusOrigin() types.CensusOrigin {
	return types.CensusOriginCSPEdDSABabyJubJubV1
}

// PublicKey returns the public key of the EdDSA instance. It returns the
// public key as a hex bytes.
func (c *BabyJubJubEdDSA) PublicKey() (CompressedBytes, error) {
	return CompressedPublicKey(c.privKey.Public())
}

// CensusRoot returns the census root computed from the public key of the
// EdDSA instance. It uses the X and Y coordinates of the public key's point
// to compute the hash. If the EdDSA signer is not initialized or the public
// key can not be converted to censusRoot for the instance curve, it returns
// nil.
func (c *BabyJubJubEdDSA) CensusRoot() *types.CensusRoot {
	// Convert the public key into a census root
	censusRoot, err := c.root()
	if err != nil {
		return nil
	}
	// Return it as a normalized census root
	return &types.CensusRoot{
		Root: censusRoot,
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
	// Sign the process ID, the address and the weight
	signature, err := c.sign(processID, address, weight)
	if err != nil {
		return nil, err
	}
	// Get the root for the current private key
	censusRoot, err := c.root()
	if err != nil {
		return nil, fmt.Errorf("error computing census root: %w", err)
	}
	// Get the public key in hex bytes
	publicKey, err := c.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("error marshaling public key: %w", err)
	}
	voterIndex := c.indexFn(processID, address, weight)
	return &types.CensusProof{
		CensusOrigin: c.CensusOrigin(),
		Root:         censusRoot,
		Address:      address.Bytes(),
		Weight:       weight,
		VoterIndex:   voterIndex,
		ProcessID:    processID,
		PublicKey:    publicKey.Bytes(),
		Signature:    signature.Bytes(),
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
	pubKey, err := DecompressPublicKey(proof.PublicKey)
	if err != nil {
		return fmt.Errorf("error getting public key from census proof: %w", err)
	}
	// Recompute the signature message
	message, err := c.signatureMessage(proof.ProcessID, proof.Address, proof.Weight)
	if err != nil {
		return fmt.Errorf("error composing signature message: %w", err)
	}
	// Verify the index
	if idx := c.indexFn(proof.ProcessID, common.BytesToAddress(proof.Address), proof.Weight); proof.VoterIndex != idx {
		return fmt.Errorf("index mismatch: expected %d, got %d", idx, proof.VoterIndex)
	}
	// Decompress the signature
	signature, err := DecompressSignature(proof.Signature)
	if err != nil {
		return fmt.Errorf("error decompressing signature: %w", err)
	}
	// Verify the signature using the public key and the message
	if verified := pubKey.VerifyPoseidon(message.BigInt().MathBigInt(), signature); !verified {
		return fmt.Errorf("signature verification failed for address %s", proof.Address.String())
	}
	return nil
}

// root function encodes the public key provided as a census root by hashing
// its coords accordingly to the curve provided. It uses the poseidon hash
// function for that curve to calculate the hash of the public key coords.
// It returns an error if the public key provided is invalid for the desired
// curve or if the curve is not supported.
func (c *BabyJubJubEdDSA) root() (types.HexBytes, error) {
	pubKey := c.privKey.Public()
	// Reset the hash function before using it
	c.hashFn.Reset()
	// Hash the public key using the poseidon hash function
	hashedPubKey, err := c.hashFn.BigIntsSum([]*big.Int{pubKey.X, pubKey.Y})
	if err != nil {
		return nil, fmt.Errorf("error hashing public key: %w", err)
	}
	return types.NormalizedCensusRoot(hashedPubKey.Bytes()), nil
}

// signatureMessage method creates a message suitable for signing using the
// current hash function to hash the process ID, the address and the weight.
func (c *BabyJubJubEdDSA) signatureMessage(
	processID types.ProcessID,
	address types.HexBytes,
	weight *types.BigInt,
) (types.HexBytes, error) {
	// Inputs validation
	if !processID.IsValid() {
		return nil, fmt.Errorf("invalid process ID")
	}
	if common.BytesToAddress(address) == (common.Address{}) {
		return nil, fmt.Errorf("invalid address")
	}
	if weight == nil || weight.LessThanOrEqual(new(types.BigInt).SetInt(0)) ||
		!weight.IsInField(bn254.ID.ScalarField()) {
		return nil, fmt.Errorf("invalid weight")
	}
	// Reset the hash function before using it
	c.hashFn.Reset()
	// Hash the process ID and address to create a message suitable for signing
	// using the poseidon hash function. Ensure that the process ID and address
	// are converted to field elements for the curve.
	message, err := c.hashFn.BigIntsSum([]*big.Int{
		processID.BigInt().MathBigInt(),
		address.BigInt().MathBigInt(),
		weight.MathBigInt(),
	})
	if err != nil {
		return nil, fmt.Errorf("error hashing signature message: %w", err)
	}
	return message.Bytes(), nil
}

// sign method signs a message with the current EdDSA private key. It returns
// the compressed signature of the message.
func (c *BabyJubJubEdDSA) sign(
	processID types.ProcessID,
	address common.Address,
	weight *types.BigInt,
) (CompressedBytes, error) {
	message, err := c.signatureMessage(processID, address.Bytes(), weight)
	if err != nil {
		return nil, fmt.Errorf("error composing signature message: %w", err)
	}
	// Compute the signature of the message
	signature := c.privKey.SignPoseidon(message.BigInt().MathBigInt())
	// Encode the signature into real hex bytes
	compressedSignature, err := CompressSignature(signature)
	if err != nil {
		return nil, fmt.Errorf("error compressing signature: %w", err)
	}
	return compressedSignature, nil
}

// DefaultCSPIndexFn is the default function to compute the VoterIndex for
// a given process ID, address and weight. It uses the poseidon hash function
// to compute a deterministic index based on the inputs. It ensures that the
// result is in the VoterIndex range [0, params.VoterIndexMax].
func DefaultCSPIndexFn(processID types.ProcessID, address common.Address, weight *types.BigInt) uint64 {
	bigHash, err := DefaultHashFn.BigIntsSum([]*big.Int{
		processID.BigInt().MathBigInt(),
		address.Big(),
		weight.MathBigInt(),
	})
	if err != nil {
		panic(err)
	}

	return new(big.Int).Mod(bigHash, new(big.Int).SetUint64(params.VoterIndexMax)).Uint64()
}
