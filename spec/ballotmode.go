package spec

import (
	"fmt"
	"math/big"
)

// BallotMode defines the ballot configuration fields used by the spec.
type BallotMode struct {
	NumFields      uint8
	GroupSize      uint8
	UniqueValues   bool
	CostFromWeight bool
	CostExponent   uint8
	MaxValue       uint64
	MinValue       uint64
	MaxValueSum    uint64
	MinValueSum    uint64
}

// Pack packs the ballot mode fields into a single field element.
func (bm BallotMode) Pack() (*big.Int, error) {
	if bm.GroupSize > bm.NumFields {
		return nil, fmt.Errorf("pack ballot mode: groupSize exceeds numFields")
	}
	if bm.MaxValue >= 1<<48 {
		return nil, fmt.Errorf("pack ballot mode: maxValue exceeds 48 bits")
	}
	if bm.MinValue >= 1<<48 {
		return nil, fmt.Errorf("pack ballot mode: minValue exceeds 48 bits")
	}
	if bm.MaxValueSum >= 1<<63 {
		return nil, fmt.Errorf("pack ballot mode: maxValueSum exceeds 63 bits")
	}
	if bm.MinValueSum >= 1<<63 {
		return nil, fmt.Errorf("pack ballot mode: minValueSum exceeds 63 bits")
	}

	packed := new(big.Int).SetUint64(uint64(bm.NumFields))
	packed.Or(packed, new(big.Int).Lsh(new(big.Int).SetUint64(uint64(bm.GroupSize)), 8))
	if bm.UniqueValues {
		packed.Or(packed, new(big.Int).Lsh(big.NewInt(1), 16))
	}
	if bm.CostFromWeight {
		packed.Or(packed, new(big.Int).Lsh(big.NewInt(1), 17))
	}
	packed.Or(packed, new(big.Int).Lsh(new(big.Int).SetUint64(uint64(bm.CostExponent)), 18))
	packed.Or(packed, new(big.Int).Lsh(new(big.Int).SetUint64(bm.MaxValue), 26))
	packed.Or(packed, new(big.Int).Lsh(new(big.Int).SetUint64(bm.MinValue), 74))
	packed.Or(packed, new(big.Int).Lsh(new(big.Int).SetUint64(bm.MaxValueSum), 122))
	packed.Or(packed, new(big.Int).Lsh(new(big.Int).SetUint64(bm.MinValueSum), 185))
	return packed, nil
}
