package eddsa

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"hash"
	"math/big"

	bls12377 "github.com/consensys/gnark-crypto/ecc/bls12-377"
	bls12377_mimc "github.com/consensys/gnark-crypto/ecc/bls12-377/fr/mimc"
	bls12377_eddsa "github.com/consensys/gnark-crypto/ecc/bls12-377/twistededwards/eddsa"
	bn254 "github.com/consensys/gnark-crypto/ecc/bn254"
	bn254_mimc "github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	bn254_eddsa "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards/eddsa"
	"github.com/consensys/gnark-crypto/ecc/twistededwards"
	"github.com/consensys/gnark-crypto/signature"
	"github.com/ethereum/go-ethereum/common"
	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/vocdoni/davinci-node/types"
)

// EdDSA struct implements the CSP interface for the EdDSA over multiple
// curves.
type EdDSA struct {
	curve  twistededwards.ID
	hashFn hash.Hash
	signer signature.Signer
}

// CSP creates a new EdDSA CSP for the specified curve. It implements the CSP
// interface and can be used to generate and verify proofs for voters. It
// initializes the hash function and generates a new private key for the curve
// with a random seed. If something goes wrong during the key generation, it
// returns an error.
func CSP(curve twistededwards.ID) (*EdDSA, error) {
	csp := new(EdDSA)
	switch curve {
	case twistededwards.BLS12_377:
		csp.curve = twistededwards.BLS12_377
		csp.hashFn = bls12377_mimc.NewMiMC()
		var err error
		if csp.signer, err = bls12377_eddsa.GenerateKey(rand.Reader); err != nil {
			return nil, fmt.Errorf("error generating private key: %w", err)
		}
	case twistededwards.BN254:
		csp.curve = twistededwards.BN254
		csp.hashFn = bn254_mimc.NewMiMC()
		var err error
		if csp.signer, err = bn254_eddsa.GenerateKey(rand.Reader); err != nil {
			return nil, fmt.Errorf("error generating private key: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported curve: %d", curve)
	}
	return csp, nil
}

// SetSeed sets the seed for the EdDSA instance. It generates a new private
// key using the provided seed. The seed must not be empty, and it is used
// to derive the private key for the curve of the EdDSA instance. If the seed
// is empty or if there is an error during key generation, it returns an error.
func (c *EdDSA) SetSeed(seed []byte) error {
	if len(seed) == 0 {
		return fmt.Errorf("seed cannot be empty")
	}
	switch c.curve {
	case twistededwards.BLS12_377:
		// set the hash function and the signer for the BLS12-377 curve
		var err error
		hashSeed := bytes.NewReader(mimc7.HashBytes(seed).Bytes())
		if c.signer, err = bls12377_eddsa.GenerateKey(hashSeed); err != nil {
			return fmt.Errorf("error generating private key: %w", err)
		}
	case twistededwards.BN254:
		// set the hash function and the signer for the BLS12-377 curve
		var err error
		hashSeed := bytes.NewReader(mimc7.HashBytes(seed).Bytes())
		if c.signer, err = bn254_eddsa.GenerateKey(hashSeed); err != nil {
			return fmt.Errorf("error generating private key: %w", err)
		}
	default:
		return fmt.Errorf("unsupported curve: %d", c.curve)
	}
	return nil
}

// CensusOrigin returns the origin of the credential service providers. It
// returns the type of the CSP, which is EdDSA in this case.
func (c *EdDSA) CensusOrigin() types.CensusOrigin {
	switch c.curve {
	case twistededwards.BLS12_377:
		return types.CensusOriginCSPEdDSABLS12377V1
	case twistededwards.BN254:
		return types.CensusOriginCSPEdDSABN254V1
	default:
		return types.CensusOriginUnknown
	}
}

// CensusRoot returns the census root computed from the public key of the
// EdDSA instance. It uses the X and Y coordinates of the public key's point
// to compute the hash. If the EdDSA signer is not initialized or the public
// key can not be converted to censusRoot for the instance curve, it returns
// nil.
func (c *EdDSA) CensusRoot() *types.CensusRoot {
	if c.signer == nil {
		return nil
	}
	censusRoot, err := pubKeyPointToCensusRoot(c.curve, c.signer.Public())
	if err != nil {
		return nil
	}
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
func (c *EdDSA) GenerateProof(
	processID *types.ProcessID,
	address common.Address,
) (*types.CensusProof, error) {
	if c.signer == nil {
		return nil, fmt.Errorf("csp is not initialized")
	}
	if processID == nil || !processID.IsValid() {
		return nil, fmt.Errorf("invalid process ID")
	}
	if address == (common.Address{}) {
		return nil, fmt.Errorf("invalid address")
	}
	// sign the message composed by the process ID and address using the hash
	// function and the private key of the CSP
	message, err := signatureMessage(c.curve, processID.Marshal(), address.Bytes())
	if err != nil {
		return nil, fmt.Errorf("error composing signature message: %w", err)
	}
	signature, err := c.signer.Sign(message, c.hashFn)
	if err != nil {
		return nil, fmt.Errorf("error signing message: %w", err)
	}
	censusRoot, err := pubKeyPointToCensusRoot(c.curve, c.signer.Public())
	if err != nil {
		return nil, fmt.Errorf("error computing census root: %w", err)
	}
	return &types.CensusProof{
		CensusOrigin: c.CensusOrigin(),
		Root:         censusRoot,
		Address:      address.Bytes(),
		ProcessID:    processID.Marshal(),
		PublicKey:    c.signer.Public().Bytes(),
		Signature:    signature,
	}, nil
}

// VerifyProof method verifies the proof provided for the current EdDSA curve.
// It also verifies that the census origin of the proof matches with the EdDSA
// instance one. It returns an error if the proof provided is nil, the census
// origins do not match or something fails during signature verification
// process.
func (c *EdDSA) VerifyProof(proof *types.CensusProof) error {
	if proof == nil {
		return fmt.Errorf("proof is nil")
	}
	if proof.CensusOrigin != c.CensusOrigin() {
		return fmt.Errorf("proof origin mismatch: expected %s, got %s", c.CensusOrigin(), proof.CensusOrigin)
	}
	// get the public key from the proof
	pubKey, err := pubKeyFromCensusProof(proof)
	if err != nil {
		return fmt.Errorf("error getting public key from census proof: %w", err)
	}
	// recompute the signature message
	message, err := signatureMessage(c.curve, proof.ProcessID, proof.Address)
	if err != nil {
		return fmt.Errorf("error composing signature message: %w", err)
	}
	// verify the signature using the public key and the message
	if verified, err := pubKey.Verify(proof.Signature, message, c.hashFn); err != nil {
		return fmt.Errorf("error verifying signature: %w", err)
	} else if !verified {
		return fmt.Errorf("signature verification failed for address %s", proof.Address.String())
	}
	return nil
}

// pubKeyFromCensusProof function returns a decoded public key from the proof
// provided acording to its census origin.
func pubKeyFromCensusProof(proof *types.CensusProof) (signature.PublicKey, error) {
	switch proof.CensusOrigin {
	case types.CensusOriginCSPEdDSABLS12377V1:
		pubKey := new(bls12377_eddsa.PublicKey)
		if _, err := pubKey.SetBytes(proof.PublicKey); err != nil {
			return nil, fmt.Errorf("error unmarshalling public key: %w", err)
		}
		return pubKey, nil
	case types.CensusOriginCSPEdDSABN254V1:
		pubKey := new(bn254_eddsa.PublicKey)
		if _, err := pubKey.SetBytes(proof.PublicKey); err != nil {
			return nil, fmt.Errorf("error unmarshalling public key: %w", err)
		}
		return pubKey, nil
	default:
		return nil, fmt.Errorf("unsupported census origin: %d", proof.CensusOrigin)
	}
}

// pubKeyPointToCensusRoot function encodes the public key provided as a census
// root by hashing its coords accordingly to the curve provided. It uses the
// mimc hash function for that curve to calculate the hash of the public key
// coords. It returns an error if the public key provided is invalid for the
// desired curve or if the curve is not supported.
func pubKeyPointToCensusRoot(
	curve twistededwards.ID,
	pubKey signature.PublicKey,
) (types.HexBytes, error) {
	switch curve {
	case twistededwards.BLS12_377:
		pk, ok := pubKey.(*bls12377_eddsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("invalid public key type: %T", pubKey)
		}
		hashedPubKey, err := mimc7.Hash([]*big.Int{
			pk.A.X.BigInt(new(big.Int)),
			pk.A.Y.BigInt(new(big.Int)),
		}, nil)
		if err != nil {
			return nil, fmt.Errorf("error hashing public key: %w", err)
		}
		return hashedPubKey.Bytes(), nil
	case twistededwards.BN254:
		pk, ok := pubKey.(*bn254_eddsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("invalid public key type: %T", pubKey)
		}
		hashedPubKey, err := mimc7.Hash([]*big.Int{
			pk.A.X.BigInt(new(big.Int)),
			pk.A.Y.BigInt(new(big.Int)),
		}, nil)
		if err != nil {
			return nil, fmt.Errorf("error hashing public key: %w", err)
		}
		return hashedPubKey.Bytes(), nil
	default:
		return nil, fmt.Errorf("unsupported curve: %d", curve)
	}
}

// signatureMessage composes the message to be signed by the CSP. The message
// is the concatenation of the process ID and address, both converted to field
// elements suitable for the circuit.
func signatureMessage(curve twistededwards.ID, pid, address types.HexBytes) ([]byte, error) {
	if len(pid) == 0 || len(address) == 0 {
		return nil, fmt.Errorf("process ID and address must not be empty")
	}
	// Hash the process ID and address to create a message suitable for signing
	// using the mimc7 hash function. Ensure that the process ID and address
	// are converted to field elements for the curve.
	res, err := mimc7.Hash([]*big.Int{
		pid.BigInt().ToFF(bn254.ID.ScalarField()).MathBigInt(),
		address.BigInt().ToFF(bn254.ID.ScalarField()).MathBigInt(),
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("error hashing signature message: %w", err)
	}
	msg := types.HexBytes(res.Bytes())
	// Convert the message to a byte slice suitable for signing
	switch curve {
	case twistededwards.BLS12_377:
		// For BLS12-377, we need to convert the message to a field element
		return msg.BigInt().ToFF(bls12377.ID.ScalarField()).Bytes(), nil
	case twistededwards.BN254:
		// For BN254, we need to convert the message to a field element
		return msg.BigInt().ToFF(bn254.ID.ScalarField()).Bytes(), nil
	default:
		return nil, fmt.Errorf("unsupported curve: %d", curve)
	}
}
