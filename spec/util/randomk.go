package util

import (
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
)

// RandomK returns randomness in the BN254 scalar field.
func RandomK() (*big.Int, error) {
	var k fr.Element
	if _, err := k.SetRandom(); err != nil {
		return nil, err
	}
	return k.BigInt(new(big.Int)), nil
}
