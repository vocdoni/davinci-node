package spec

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/spec/params"
)

// BallotMode defines the ballot configuration fields used by the spec.
type BallotMode struct {
	NumFields    uint8  `json:"numFields" cbor:"0,keyasint,omitempty"`
	GroupSize    uint8  `json:"groupSize" cbor:"1,keyasint,omitempty"`
	UniqueValues bool   `json:"uniqueValues" cbor:"2,keyasint,omitempty"`
	CostExponent uint8  `json:"costExponent" cbor:"3,keyasint,omitempty"`
	MaxValue     uint64 `json:"maxValue" cbor:"4,keyasint,omitempty"`
	MinValue     uint64 `json:"minValue" cbor:"5,keyasint,omitempty"`
	MaxValueSum  uint64 `json:"maxValueSum" cbor:"6,keyasint,omitempty"`
	MinValueSum  uint64 `json:"minValueSum" cbor:"7,keyasint,omitempty"`
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
	packed.Or(packed, new(big.Int).Lsh(new(big.Int).SetUint64(uint64(bm.CostExponent)), 17))
	packed.Or(packed, new(big.Int).Lsh(new(big.Int).SetUint64(bm.MaxValue), 25))
	packed.Or(packed, new(big.Int).Lsh(new(big.Int).SetUint64(bm.MinValue), 73))
	packed.Or(packed, new(big.Int).Lsh(new(big.Int).SetUint64(bm.MaxValueSum), 121))
	packed.Or(packed, new(big.Int).Lsh(new(big.Int).SetUint64(bm.MinValueSum), 184))
	return packed, nil
}

// Validate validates the ballot mode fields for basic consistency.
func (bm BallotMode) Validate() error {
	if int(bm.NumFields) > params.FieldsPerBallot {
		return fmt.Errorf("numFields %d is greater than max size %d", bm.NumFields, params.FieldsPerBallot)
	}
	if bm.GroupSize > bm.NumFields {
		return fmt.Errorf("groupSize %d exceeds numFields %d", bm.GroupSize, bm.NumFields)
	}
	if bm.MinValue > bm.MaxValue {
		return fmt.Errorf("minValue %d is greater than maxValue %d", bm.MinValue, bm.MaxValue)
	}
	if bm.MinValueSum > bm.MaxValueSum {
		return fmt.Errorf("minValueSum %d is greater than maxValueSum %d", bm.MinValueSum, bm.MaxValueSum)
	}
	return nil
}

// String returns a string representation of the ballot mode.
func (bm BallotMode) String() string {
	data, err := json.Marshal(bm)
	if err != nil {
		return ""
	}
	return string(data)
}
