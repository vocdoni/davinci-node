package main

import (
	"fmt"
	"math/big"

	"github.com/iden3/go-iden3-crypto/poseidon"
)

const bn254ScalarField = "21888242871839275222246405745257275088548364400416034343698204186575808495617"

// GenCommitmentAndNullifier generates a commitment and nullifier for the
// given address, processID and secret values. It uses the Poseidon hash
// function over BabyJubJub curve to generate the commitment and nullifier.
// The commitment is generated using the address, processID and secret value,
// while the nullifier is generated using the commitment and secret value.
func GenCommitmentAndNullifier(address, processID, secret []byte) (*big.Int, *big.Int, error) {
	scalarField, ok := new(big.Int).SetString(bn254ScalarField, 10)
	if !ok {
		return nil, nil, fmt.Errorf("failed to set bn254 scalar field")
	}
	commitment, err := poseidon.Hash([]*big.Int{
		BigToFF(scalarField, new(big.Int).SetBytes(address)),
		BigToFF(scalarField, new(big.Int).SetBytes(processID)),
		BigToFF(scalarField, new(big.Int).SetBytes(secret)),
	})
	if err != nil {
		return nil, nil, err
	}
	nullifier, err := poseidon.Hash([]*big.Int{
		commitment,
		BigToFF(scalarField, new(big.Int).SetBytes(secret)),
	})
	if err != nil {
		return nil, nil, err
	}
	return commitment, nullifier, nil
}
