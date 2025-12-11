// Package poseidon provides cryptographic hash functions based on the Poseidon hash algorithm.
// It includes utilities for hashing large numbers of inputs efficiently.
package poseidon

import (
	"fmt"
	"math/big"

	"github.com/iden3/go-iden3-crypto/poseidon"
)

// MultiPoseidon computes the Poseidon hash of a variable number of big.Int inputs.
// It handles large numbers of inputs by chunking them into groups of 16, hashing each chunk,
// and then recursively hashing the resulting hashes together. This allows for efficient hashing
// of large input sets (including full 4096-element blobs) while maintaining the security
// properties of the Poseidon hash function.
// Returns an error if no inputs are provided.
func MultiPoseidon(inputs ...*big.Int) (*big.Int, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("no inputs provided")
	}

	// For 16 or fewer inputs, hash directly
	if len(inputs) <= 16 {
		return poseidon.Hash(inputs)
	}

	// Pre-calculate number of chunks for memory efficiency
	numChunks := (len(inputs) + 15) / 16 // ceiling division
	hashes := make([]*big.Int, 0, numChunks)

	// Process inputs in 16-element chunks
	for i := 0; i < len(inputs); i += 16 {
		end := min(i+16, len(inputs))

		hash, err := poseidon.Hash(inputs[i:end])
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
	}

	// Single chunk case - return directly
	if len(hashes) == 1 {
		return hashes[0], nil
	}

	// Multiple chunks - recursively hash chunk hashes if needed
	// If we have more than 16 chunk hashes, recursively apply MultiPoseidon
	if len(hashes) <= 16 {
		return poseidon.Hash(hashes)
	}

	// Recursively hash the chunk hashes
	return MultiPoseidon(hashes...)
}
