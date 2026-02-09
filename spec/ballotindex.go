package spec

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/spec/hash"
	"github.com/vocdoni/davinci-node/spec/params"
)

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
