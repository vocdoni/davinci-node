package spec

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/spec/hash"
	"github.com/vocdoni/davinci-node/spec/params"
)

// VoteID calculates the poseidon hash of:
// the process ID, voter's address and a secret value k.
// This is truncated to the least significant 63 bits,
// and then shifted to the upper half of the 64 bit space
// by setting the first bit to 1.
func VoteID(processID, address, k *big.Int) (uint64, error) {
	voteID, err := hash.VoteID(processID, address, k)
	if err != nil {
		return 0, err
	}
	if !voteID.IsUint64() {
		return 0, fmt.Errorf("vote ID overflows uint64")
	}
	return voteID.Uint64(), nil
}

// BallotIndex returns a BallotIndex on the lower half of the 64 bit space,
// between BallotMin and BallotMax.
//
//	BallotIndex = BallotMin + (censusIndex * 2^CensusAddressBitLen) + (address mod 2^CensusAddressBitLen)
func BallotIndex(address *big.Int, censusIndex uint64) (uint64, error) {
	if censusIndex > params.CensusIndexMax {
		return 0, fmt.Errorf("censusIndex too big")
	}
	censusIndexShifted := censusIndex * (1 << params.CensusAddressBitLen)
	addressTruncated := hash.TruncateToLowerBits(address, params.CensusAddressBitLen)
	return params.BallotMin + censusIndexShifted + addressTruncated.Uint64(), nil
}
