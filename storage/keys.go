package storage

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/crypto/ecc"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// SetEncryptionKeys stores the encryption keys for a process.
func (s *Storage) SetEncryptionKeys(pid *types.ProcessID, publicKey ecc.Point, privateKey *big.Int) error {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	return s.setEncryptionKeysUnsafe(pid, publicKey, privateKey)
}

// EncryptionKeys loads the encryption keys for a process. Returns ErrNotFound if the keys do not exist
func (s *Storage) EncryptionKeys(pid *types.ProcessID) (ecc.Point, *big.Int, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	return s.encryptionKeysUnsafe(pid)
}

// FetchOrGenerateEncryptionKeys loads the encryption keys for a process.
// If the keys do not exist, new ones are generated and persisted to storage.
func (s *Storage) FetchOrGenerateEncryptionKeys(pid *types.ProcessID) (ecc.Point, *big.Int, error) {
	s.globalLock.Lock()
	defer s.globalLock.Unlock()
	return s.fetchOrGenerateEncryptionKeysUnsafe(pid)
}

// setEncryptionKeysUnsafe stores both the private and public encryption keys for a process, without locking.
func (s *Storage) setEncryptionKeysUnsafe(pid *types.ProcessID, publicKey ecc.Point, privateKey *big.Int) error {
	x, y := publicKey.Point()
	eks := &EncryptionKeys{
		X:          x,
		Y:          y,
		PrivateKey: privateKey,
	}
	return s.setArtifact(encryptionKeyPrefix, pid.Marshal(), eks)
}

// setEncryptionPubKeyUnsafe stores only the encryption public key for a process, without locking.
// If there's already a matching EncryptionKey (PubKey) in storage, it won't rewrite it.
func (s *Storage) setEncryptionPubKeyUnsafe(pid *types.ProcessID, ek *types.EncryptionKey) error {
	publicKey, _, err := s.encryptionKeysUnsafe(pid)
	if err == nil {
		if types.EncryptionKeyFromPoint(publicKey).X.Equal(ek.X) &&
			types.EncryptionKeyFromPoint(publicKey).Y.Equal(ek.Y) {
			return nil
		}
		log.Warnf("stored encryption key for process %s mismatch, overwriting stored %+v with new %+v", pid.String(),
			types.EncryptionKeyFromPoint(publicKey), ek)
	}

	eks := &EncryptionKeys{
		X: ek.X.MathBigInt(),
		Y: ek.Y.MathBigInt(),
	}
	return s.setArtifact(encryptionKeyPrefix, pid.Marshal(), eks)
}

// encryptionKeysUnsafe loads the encryption keys for a process without locking.
func (s *Storage) encryptionKeysUnsafe(pid *types.ProcessID) (ecc.Point, *big.Int, error) {
	eks := new(EncryptionKeys)
	if err := s.getArtifact(encryptionKeyPrefix, pid.Marshal(), eks); err != nil {
		return nil, nil, err
	}
	if eks.X == nil || eks.Y == nil {
		return nil, nil, fmt.Errorf("not found or malformed encryption keys")
	}

	pubKey := curves.New(bjj.CurveType).SetPoint(eks.X, eks.Y)
	return pubKey, eks.PrivateKey, nil
}

// fetchOrGenerateEncryptionKeysUnsafe loads the encryption keys for a process.
// If the keys do not exist, new ones are generated and persisted to storage.
// It does not lock the storage, so it should be used with caution.
func (s *Storage) fetchOrGenerateEncryptionKeysUnsafe(pid *types.ProcessID) (ecc.Point, *big.Int, error) {
	publicKey, privateKey, err := s.encryptionKeysUnsafe(pid)
	if err != nil {
		publicKey, privateKey, err = elgamal.GenerateKey(curves.New(bjj.CurveType))
		if err != nil {
			return nil, nil, fmt.Errorf("could not generate elgamal key: %v", err)
		}
		if err := s.setEncryptionKeysUnsafe(pid, publicKey, privateKey); err != nil {
			return nil, nil, fmt.Errorf("could not store encryption keys: %v", err)
		}
	}
	return publicKey, privateKey, nil
}
