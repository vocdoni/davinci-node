package hash

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/spec/params"
)

// VoteID calculates the poseidon hash of processID, address, and k.
// The hash is truncated to VoteIDHashBits and shifted into the upper half.
func VoteID(processID, address, k *big.Int) (*big.Int, error) {
	if processID == nil || address == nil || k == nil {
		return nil, fmt.Errorf("processID, address, and k are required")
	}
	baseField := params.BallotProofCurve.ScalarField()
	h, err := PoseidonHash(
		bigToFF(baseField, processID),
		bigToFF(baseField, address),
		bigToFF(baseField, k),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate vote ID: %w", err)
	}
	truncated := TruncateToLowerBits(h, params.VoteIDHashBits)
	return new(big.Int).Add(new(big.Int).SetUint64(params.VoteIDMin), truncated), nil
}

// TruncateToLowerBits returns a big.Int truncated to the least-significant `bits`.
func TruncateToLowerBits(input *big.Int, bits uint) *big.Int {
	mask := new(big.Int).Lsh(big.NewInt(1), bits) // 1 << bits
	mask.Sub(mask, big.NewInt(1))                 // (1 << bits) - 1
	return new(big.Int).And(input, mask)          // input & ((1 << bits) - 1)
}

func bigToFF(field, value *big.Int) *big.Int {
	z := big.NewInt(0)
	if c := value.Cmp(field); c == 0 {
		return z
	} else if c != 1 && value.Cmp(z) != -1 {
		return value
	}
	return z.Mod(value, field)
}
