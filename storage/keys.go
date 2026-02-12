package storage

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/crypto/ecc"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/types"
)

const encKeyCurveType = bjj.CurveType

// ProcessEncryptionKeys loads the encryption keys for a process. It checks for
// the public key in storage and then tries to load the full encryption keys.
// If the encryption keys are not found, it returns an error.
func (s *Storage) ProcessEncryptionKeys(processID types.ProcessID) (ecc.Point, *big.Int, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	// Get the process from storage
	process, err := s.process(processID)
	if err != nil {
		return nil, nil, err
	}
	// Check if the process has encryption keys
	if process.EncryptionKey == nil {
		return nil, nil, ErrNotFound
	}
	// Return the encryption keys by public key
	return s.encryptionKeysUnsafe(ProcessEncryptionKeyToPoint(process.EncryptionKey))
}

// GenerateProcessEncryptionKeys generates a new encryption key pair and stores
// it in storage by using the public key as the key.
func (s *Storage) GenerateProcessEncryptionKeys() (ecc.Point, *big.Int, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	return s.generateEncryptionKeysUnsafe()
}

// setEncryptionKeysUnsafe stores the given encryption public and private keys,
// without locking the storage and using the public key as the key in storage.
func (s *Storage) setEncryptionKeysUnsafe(publicKey ecc.Point, privateKey *big.Int) error {
	x, y := publicKey.Point()
	eks := &EncryptionKeys{
		X:          x,
		Y:          y,
		PrivateKey: privateKey,
	}
	return s.setArtifact(encryptionKeyPrefix, publicKey.Marshal(), eks)
}

// setEncryptionPubKeyUnsafe stores the given encryption public key, without
// locking the storage and using the public key as the key and nil private key
// as the value in storage.
func (s *Storage) setEncryptionPubKeyUnsafe(publicKey ecc.Point) error {
	// Check if the encryption keys already exist
	if _, _, err := s.encryptionKeysUnsafe(publicKey); err == nil {
		return nil
	}
	// If not, create them but just with the public key
	return s.setEncryptionKeysUnsafe(publicKey, nil)
}

// encryptionKeysUnsafe loads the encryption keys for the given public key,
// without locking the storage.
func (s *Storage) encryptionKeysUnsafe(publicKey ecc.Point) (ecc.Point, *big.Int, error) {
	eks := new(EncryptionKeys)
	if err := s.getArtifact(encryptionKeyPrefix, publicKey.Marshal(), eks); err != nil {
		return nil, nil, err
	}
	if eks.X == nil || eks.Y == nil {
		return nil, nil, fmt.Errorf("not found or malformed encryption keys")
	}
	return eks.Point(), eks.PrivateKey, nil
}

// generateEncryptionKeysUnsafe generates a new encryption key pair, using
// Baby-Jubjub as the curve, and stores them in the storage. It does not lock
// the storage, so it should be used with caution. It returns the public and
// private keys, and an error if the keys could not be generated or stored.
func (s *Storage) generateEncryptionKeysUnsafe() (ecc.Point, *big.Int, error) {
	publicKey, privateKey, err := elgamal.GenerateKey(curves.New(encKeyCurveType))
	if err != nil {
		return nil, nil, fmt.Errorf("could not generate elgamal key: %v", err)
	}
	if err := s.setEncryptionKeysUnsafe(publicKey, privateKey); err != nil {
		return nil, nil, fmt.Errorf("could not store encryption keys: %v", err)
	}
	return publicKey, privateKey, nil
}

// ProcessEncryptionKeyToPoint converts a process encryption key to an ecc.Point
// using the Baby-Jubjub curve.
func ProcessEncryptionKeyToPoint(pk *types.EncryptionKey) ecc.Point {
	eks := &EncryptionKeys{
		X: pk.X.MathBigInt(),
		Y: pk.Y.MathBigInt(),
	}
	return eks.Point()
}
