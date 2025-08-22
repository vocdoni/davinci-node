package storage

import "crypto/sha256"

func hashKey(data []byte) []byte {
	hash := sha256.Sum256(data)
	return hash[:maxKeySize]
}
