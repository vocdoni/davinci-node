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

// SetEncryptionKeys stores the encryption keys for a process.
func (s *Storage) SetEncryptionKeys(pid *types.ProcessID, publicKey ecc.Point, privateKey *big.Int) error {
	x, y := publicKey.Point()
	eks := &EncryptionKeys{
		X:          x,
		Y:          y,
		PrivateKey: privateKey,
	}
	return s.setArtifact(encryptionKeyPrefix, pid.Marshal(), eks)
}

// EncryptionKeys loads the encryption keys for a process. Returns ErrNotFound if the keys do not exist
func (s *Storage) EncryptionKeys(pid *types.ProcessID) (ecc.Point, *big.Int, error) {
	eks := new(EncryptionKeys)
	err := s.getArtifact(encryptionKeyPrefix, pid.Marshal(), eks)
	if err != nil {
		return nil, nil, err
	}
	if eks.X == nil || eks.Y == nil {
		return nil, nil, fmt.Errorf("not found or malformed encryption keys")
	}

	pubKey := curves.New(bjj.CurveType).SetPoint(eks.X, eks.Y)
	return pubKey, eks.PrivateKey, nil
}

// FetchOrGenerateEncryptionKeys loads the encryption keys for a process.
// If the keys do not exist, new ones are generated and persisted to storage.
func (s *Storage) FetchOrGenerateEncryptionKeys(pid *types.ProcessID) (ecc.Point, *big.Int, error) {
	publicKey, privateKey, err := s.EncryptionKeys(pid)
	if err != nil {
		publicKey, privateKey, err = elgamal.GenerateKey(curves.New(bjj.CurveType))
		if err != nil {
			return nil, nil, fmt.Errorf("could not generate elgamal key: %v", err)
		}
		if err := s.SetEncryptionKeys(pid, publicKey, privateKey); err != nil {
			return nil, nil, fmt.Errorf("could not store encryption keys: %v", err)
		}
	}
	return publicKey, privateKey, nil
}
