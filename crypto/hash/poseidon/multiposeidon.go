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
// and then hashing the resulting hashes together. This allows for efficient hashing of
// large input sets while maintaining the security properties of the Poseidon hash function.
// Returns an error if more than 256 inputs are provided or if no inputs are provided.
func MultiPoseidon(inputs ...*big.Int) (*big.Int, error) {
	if len(inputs) > 256 {
		return nil, fmt.Errorf("too many inputs")
	} else if len(inputs) == 0 {
		return nil, fmt.Errorf("no inputs provided")
	}
	// calculate chunk hashes
	hashes := []*big.Int{}
	chunk := []*big.Int{}
	for _, input := range inputs {
		if len(chunk) == 16 {
			hash, err := poseidon.Hash(chunk)
			if err != nil {
				return nil, err
			}
			hashes = append(hashes, hash)
			chunk = []*big.Int{}
		}
		chunk = append(chunk, input)
	}
	// if the final chunk is not empty, hash it to get the last chunk hash
	if len(chunk) > 0 {
		hash, err := poseidon.Hash(chunk)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
	}
	// if there is only one chunk hash, return it
	if len(hashes) == 1 {
		return hashes[0], nil
	}
	// return the hash of all chunk hashes
	return poseidon.Hash(hashes)
}
