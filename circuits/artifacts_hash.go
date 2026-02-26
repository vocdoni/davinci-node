package circuits

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

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
