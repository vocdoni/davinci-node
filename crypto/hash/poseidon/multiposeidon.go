// Package poseidon provides cryptographic hash functions based on the Poseidon hash algorithm.
// It includes utilities for hashing large numbers of inputs efficiently.
package poseidon

import (
	"fmt"
	"math/big"

	"github.com/iden3/go-iden3-crypto/poseidon"
	"github.com/iden3/go-iden3-crypto/utils"
	"github.com/vocdoni/davinci-node/log"
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
		hash, err := poseidon.Hash(inputs)
		log.Warnf("\n  poseidon.Hash(%+v) \n = %d = %x (%v)", inputs, hash, utils.SwapEndianness(hash.Bytes()), err)
		return hash, err
	}

	// Pre-calculate number of chunks for memory efficiency
	numChunks := (len(inputs) + 15) / 16 // ceiling division
	hashes := make([]*big.Int, 0, numChunks)

	// Process inputs in 16-element chunks
	for i := 0; i < len(inputs); i += 16 {
		end := min(i+16, len(inputs))

		hash, err := poseidon.Hash(inputs[i:end])
		log.Warnf("\n  poseidon.Hash(%+v) \n = %d = %x (%v)", inputs[i:end], hash, utils.SwapEndianness(hash.Bytes()), err)
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
		hash, err := poseidon.Hash(hashes)
		log.Warnf("\n  poseidon.Hash(%+v) \n = %d = %x (%v)", hashes, hash, utils.SwapEndianness(hash.Bytes()), err)
		return hash, err
	}

	// Recursively hash the chunk hashes
	return MultiPoseidon(hashes...)
}
