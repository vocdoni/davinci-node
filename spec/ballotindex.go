package spec

import (
	"fmt"

	"github.com/vocdoni/davinci-node/spec/params"
)

// BallotIndex returns a BallotIndex on the lower half of the 64 bit space,
// between BallotMin and BallotMax.
//
//	BallotIndex = BallotMin + censusIndex
func BallotIndex(censusIndex uint64) (uint64, error) {
	if params.BallotMin+censusIndex > params.BallotMax {
		return 0, fmt.Errorf("censusIndex too big")
	}
	return params.BallotMin + censusIndex, nil
}
