package secies

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/crypto/ecc"
)

// ScalarECIES encapsulates methods for encryption and decryption of a scalar, using elliptic curve cryptography.
type ScalarECIES struct {
	privateKey *big.Int
	publicKey  ecc.Point
	curvePoint ecc.Point
	hashFunc   func([]byte) [32]byte
}

// New initializes a new ScalarECIES instance and generates keys if privateKey is nil.
// The curve parameter is an instance of the elliptic curve group.
// The hashFunc parameter is the hash function used to derive shared secrets. If nil, SHA-256 is used.
func New(privateKey *big.Int, curve ecc.Point, hashFunc func([]byte) [32]byte) (*ScalarECIES, error) {
	if curve == nil {
		return nil, fmt.Errorf("curve cannot be nil")
	}
	se := &ScalarECIES{
		curvePoint: curve,
		hashFunc:   hashFunc,
	}
	if hashFunc == nil {
		se.hashFunc = sha256.Sum256
	}
	if privateKey == nil {
		err := se.generateKeys()
		if err != nil {
			return nil, err
		}
	} else {
		se.privateKey = privateKey
		publicKey := curve.New()
		publicKey.ScalarBaseMult(privateKey)
		se.publicKey = publicKey
	}
	return se, nil
}

// generateKeys generates a new private and public key pair.
func (se *ScalarECIES) generateKeys() error {
	order := se.curvePoint.Order()
	// Generate a random private key in [1, order-1]
	privateKey, err := rand.Int(rand.Reader, order)
	if err != nil {
		return err
	}
	if privateKey.Sign() == 0 {
		privateKey.Add(privateKey, big.NewInt(1)) // Ensure privateKey != 0
	}
	se.privateKey = privateKey

	// Compute publicKey = privateKey * G
	publicKey := se.curvePoint.New()
	publicKey.SetGenerator()
	publicKey.ScalarMult(publicKey, privateKey)
	se.publicKey = publicKey
	return nil
}

// GetPublicKey returns the marshaled public key.
func (se *ScalarECIES) GetPublicKey() []byte {
	return se.publicKey.Marshal()
}

// GetPrivateKey returns the private key.
func (se *ScalarECIES) GetPrivateKey() *big.Int {
	return se.privateKey
}

// Encrypt encrypts a message (scalar) using the recipient's public key.
func (se *ScalarECIES) Encrypt(message *big.Int, recipientPublicKey ecc.Point) (*big.Int, []byte, error) {
	order := se.curvePoint.Order()
	// Ensure message is within Fr
	message.Mod(message, order)

	// Generate ephemeral scalar r
	r, err := rand.Int(rand.Reader, order)
	if err != nil {
		return nil, nil, err
	}
	if r.Sign() == 0 {
		r.Add(r, big.NewInt(1)) // Ensure r != 0
	}

	// Compute R = r * G
	R := se.curvePoint.New()
	R.ScalarBaseMult(r)

	// Compute shared secret point S = r * recipientPublicKey
	S := se.curvePoint.New()
	S.ScalarMult(recipientPublicKey, r)

	// Hash S to get shared secret scalar s
	s := se.hashPointToScalar(S)

	// Compute ciphertext c = message + s mod Fr
	c := new(big.Int).Add(message, s)
	c.Mod(c, order)

	// Return c and R
	RBytes := R.Marshal()
	return c, RBytes, nil
}

// Decrypt decrypts a message given the ciphertext components.
func (se *ScalarECIES) Decrypt(c *big.Int, RBytes []byte) (*big.Int, error) {
	// Unmarshal R
	R := se.curvePoint.New()
	if err := R.Unmarshal(RBytes); err != nil {
		return nil, err
	}

	// Compute shared secret point S = sk * R
	S := se.curvePoint.New()
	S.ScalarMult(R, se.privateKey)

	// Hash S to get shared secret scalar s
	s := se.hashPointToScalar(S)

	// Recover message m = c - s mod Fr
	order := se.curvePoint.Order()
	m := new(big.Int).Sub(c, s)
	m.Mod(m, order)

	return m, nil
}

// hashPointToScalar hashes an elliptic curve point to a scalar in Fr.
func (se *ScalarECIES) hashPointToScalar(point ecc.Point) *big.Int {
	// Marshal the point to bytes
	pointBytes := point.Marshal()
	// Hash the bytes
	hashBytes := se.hashFunc(pointBytes)
	// Convert the hash to a big.Int
	hashInt := new(big.Int).SetBytes(hashBytes[:])
	// Reduce modulo the curve order
	hashInt.Mod(hashInt, point.Order())
	return hashInt
}
