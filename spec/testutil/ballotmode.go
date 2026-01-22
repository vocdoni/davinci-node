package testutil

import "github.com/vocdoni/davinci-node/spec"

const (
	BallotNumFields      = 6
	BallotGroupSize      = 2
	BallotUniqueValues   = 0
	BallotMaxValue       = 16
	BallotMinValue       = 0
	BallotMaxValueSum    = 1280 // (maxValue ^ costExponent) * numFields
	BallotMinValueSum    = BallotNumFields
	BallotCostExponent   = 2
	BallotCostFromWeight = 0
)

// FixedBallotMode returns a fixed ballot mode fixture used in tests.
func FixedBallotMode() spec.BallotMode {
	return spec.BallotMode{
		NumFields:      BallotNumFields,
		GroupSize:      BallotGroupSize,
		UniqueValues:   BallotUniqueValues == 1,
		MaxValue:       BallotMaxValue,
		MinValue:       BallotMinValue,
		MaxValueSum:    BallotMaxValueSum,
		MinValueSum:    BallotMinValueSum,
		CostExponent:   BallotCostExponent,
		CostFromWeight: BallotCostFromWeight == 1,
	}
}
