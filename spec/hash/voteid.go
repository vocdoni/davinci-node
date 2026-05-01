package hash

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/spec/params"
)

// VoteID calculates the poseidon hash of processID, address, and k.
// The hash is truncated to VoteIDHashBits and shifted into the upper half.
func VoteID(processID, address, k *big.Int) (*big.Int, error) {
	for _, bi := range []*big.Int{processID, address, k} {
		if bi == nil {
			return nil, fmt.Errorf("processID, address, and k are required")
		}
		if bi.Sign() < 0 || bi.Cmp(params.BallotProofCurve.ScalarField()) >= 0 {
			return nil, fmt.Errorf("processID, address, and k must be in field")
		}
	}
	h, err := PoseidonHash(processID, address, k)
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
