package workers

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
)

// WorkerSeedToUUID converts a worker seed string into a UUID. It uses the
// first 16 bytes of the SHA256 hash of the seed to create a UUID.
func WorkerSeedToUUID(seed string) (*uuid.UUID, error) {
	var err error
	// Use the first 8 characters of the SHA256 hash of the seed as a UUID
	hash := sha256.Sum256([]byte(seed))
	u, err := uuid.FromBytes(hash[:16]) // Convert first 16 bytes to UUID
	if err != nil {
		return nil, fmt.Errorf("failed to create worker UUID: %w", err)
	}
	return &u, nil
}

// ValidWorkerAddress checks if the provided address is a valid Ethereum address.
func ValidWorkerAddress(address string) (common.Address, error) {
	if common.IsHexAddress(address) {
		return common.HexToAddress(address), nil
	}
	return common.Address{}, fmt.Errorf("invalid Ethereum address: %s", address)
}

// WorkerNameFromAddress generates a worker name by obfuscating the first
// 32 characters of the provided Ethereum address as string.
func WorkerNameFromAddress(address string) (string, error) {
	if len(address) < 42 {
		return "", fmt.Errorf("invalid address length: %d, expected at least 40 characters", len(address))
	}
	// Obfuscate the first 16 characters of the address
	return strings.Repeat("*", 40) + address[40:], nil
}
