package circuits

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
)

// HashConstraintSystem returns the SHA256 hash of a constraint system.
func HashConstraintSystem(cs constraint.ConstraintSystem) (string, error) {
	hasher := sha256.New()
	if _, err := cs.WriteTo(hasher); err != nil {
		return "", fmt.Errorf("write ccs to hasher: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// HashBytesSHA256 returns the SHA256 hash of the provided byte slice.
func HashBytesSHA256(content []byte) (string, error) {
	hasher := sha256.New()
	if _, err := hasher.Write(content); err != nil {
		return "", fmt.Errorf("hash bytes: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// LoadVerifyingKeyFromLocalHash loads a verifying key from the local artifacts
// cache path using its hex hash.
func LoadVerifyingKeyFromLocalHash(curve ecc.ID, hash string) (groth16.VerifyingKey, error) {
	path := filepath.Join(BaseDir, hash)
	fd, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open verifying key file %s: %w", path, err)
	}
	defer func() {
		_ = fd.Close()
	}()

	vk := groth16.NewVerifyingKey(curve)
	if _, err := vk.ReadFrom(fd); err != nil {
		return nil, fmt.Errorf("read verifying key file %s: %w", path, err)
	}
	return vk, nil
}
