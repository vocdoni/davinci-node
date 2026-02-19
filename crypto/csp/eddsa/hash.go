package eddsa

import (
	"fmt"
	"hash"
	"math/big"

	"github.com/iden3/go-iden3-crypto/poseidon"
)

// Hash is an interface for a hash function that can be used by the
// BabyJubJubEdDSA.
type Hash interface {
	hash.Hash
	BigIntsSum([]*big.Int) (*big.Int, error)
}

// DefaultHashFn is the default hash function used by the BabyJubJubEdDSA
var DefaultHashFn Hash

func init() {
	// Initialize the default hash function to Poseidon
	var err error
	DefaultHashFn, err = NewPoseidon()
	if err != nil {
		panic(err)
	}
}

// Poseidon is a hash function based on the Poseidon hash algorithm, it wraps
// the iden3-crypto implementation to conform the Hash interface (hash.Hash
// extended with the BigIntsSum method for BigInts).
type Poseidon struct {
	hasher hash.Hash
}

// NewPoseidon returns a new Poseidon extended hash function.
func NewPoseidon() (Hash, error) {
	hasher, err := poseidon.New(6)
	if err != nil {
		return nil, fmt.Errorf("error initializing iden3 poseidon hash: %w", err)
	}
	return &Poseidon{
		hasher: hasher,
	}, nil
}

// Write method implements the hash.Hash interface. It wraps the iden3-crypto
// Poseidon hasher Write method.
func (p *Poseidon) Write(b []byte) (int, error) {
	return p.hasher.Write(b)
}

// Sum method implements the hash.Hash interface. It wraps the iden3-crypto
// Poseidon hasher Sum method.
func (p *Poseidon) Sum(b []byte) []byte {
	return p.hasher.Sum(b)
}

// Reset method implements the hash.Hash interface. It wraps the iden3-crypto
// Poseidon hasher Reset method.
func (p *Poseidon) Reset() {
	p.hasher.Reset()
}

// Size method implements the hash.Hash interface. It wraps the iden3-crypto
// Poseidon hasher Size method.
func (p *Poseidon) Size() int {
	return p.hasher.Size()
}

// BlockSize method implements the hash.Hash interface. It wraps the
// iden3-crypto Poseidon hasher BlockSize method.
func (p *Poseidon) BlockSize() int {
	return p.hasher.BlockSize()
}

// BigIntsSum method implements the additional method for the Hash interface
// that wraps the iden3-crypto Poseidon hasher for computing the Poseidon hash
// for a list of big integers.
func (p *Poseidon) BigIntsSum(i []*big.Int) (*big.Int, error) {
	return poseidon.Hash(i)
}
