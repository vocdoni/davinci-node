package spec

import (
	"fmt"

	"github.com/vocdoni/davinci-node/spec/params"
)

// BallotIndex returns a BallotIndex on the lower half of the 64 bit space,
// between BallotMin and BallotMax.
//
//	BallotIndex = BallotMin + voterIndex
func BallotIndex(voterIndex uint64) (uint64, error) {
	if voterIndex > params.VoterIndexMax {
		return 0, fmt.Errorf("voterIndex too big")
	}
	return params.BallotMin + voterIndex, nil
}
