package hash

import (
	"fmt"
	"math/big"

	"github.com/iden3/go-iden3-crypto/poseidon"
)

// PoseidonHash hashes the provided inputs with iden3 Poseidon.
func PoseidonHash(inputs ...*big.Int) (*big.Int, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("no inputs provided")
	}
	for i, v := range inputs {
		if v == nil {
			return nil, fmt.Errorf("nil input at index %d", i)
		}
	}
	return poseidon.Hash(inputs)
}

// PoseidonMultiHash matches the multiposeidon logic used in circuits (16-wide chunks).
func PoseidonMultiHash(inputs []*big.Int) (*big.Int, error) {
	if len(inputs) <= 16 {
		return PoseidonHash(inputs...)
	}
	var intermediate []*big.Int
	for i := 0; i < len(inputs); i += 16 {
		end := min(i+16, len(inputs))
		h, err := PoseidonHash(inputs[i:end]...)
		if err != nil {
			return nil, err
		}
		intermediate = append(intermediate, h)
	}
	return PoseidonHash(intermediate...)
}
