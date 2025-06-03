package storage

import (
	"crypto/sha256"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

// EncodeArtifact encodes an artifact into CBOR format.
func EncodeArtifact(a any) ([]byte, error) {
	encOpts := cbor.CoreDetEncOptions()
	em, err := encOpts.EncMode()
	if err != nil {
		return nil, fmt.Errorf("encode artifact: %w", err)
	}
	return em.Marshal(a)
}

// DecodeArtifact decodes a CBOR-encoded artifact into the provided output variable.
func DecodeArtifact(data []byte, out any) error {
	return cbor.Unmarshal(data, out)
}

func hashKey(data []byte) []byte {
	hash := sha256.Sum256(data)
	return hash[:maxKeySize]
}
