package storage

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/curves"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
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
