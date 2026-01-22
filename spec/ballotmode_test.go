package spec

import (
	"math/big"
	"testing"
)

func TestBallotModePackFields(t *testing.T) {
	bm := BallotMode{
		NumFields:      5,
		GroupSize:      3,
		UniqueValues:   true,
		CostFromWeight: false,
		CostExponent:   7,
		MaxValue:       0x123456789a,
		MinValue:       0x2233,
		MaxValueSum:    (1 << 62) + 123,
		MinValueSum:    0x1abc,
	}

	packed, err := bm.Pack()
	if err != nil {
		t.Fatalf("Pack error: %v", err)
	}

	assertField(t, packed, 0, 8, 5)
	assertField(t, packed, 8, 8, 3)
	assertField(t, packed, 16, 1, 1)
	assertField(t, packed, 17, 1, 0)
	assertField(t, packed, 18, 8, 7)
	assertField(t, packed, 26, 48, 0x123456789a)
	assertField(t, packed, 74, 48, 0x2233)
	assertField(t, packed, 122, 63, (1<<62)+123)
	assertField(t, packed, 185, 63, 0x1abc)
}

func TestBallotModePackErrors(t *testing.T) {
	cases := []struct {
		name string
		bm   BallotMode
	}{
		{
			name: "groupSize too large",
			bm: BallotMode{
				NumFields: 3,
				GroupSize: 4,
			},
		},
		{
			name: "maxValue overflows",
			bm: BallotMode{
				NumFields: 3,
				GroupSize: 3,
				MaxValue:  1 << 48,
			},
		},
		{
			name: "minValue overflows",
			bm: BallotMode{
				NumFields: 3,
				GroupSize: 3,
				MinValue:  1 << 48,
			},
		},
		{
			name: "maxValueSum overflows",
			bm: BallotMode{
				NumFields:   3,
				GroupSize:   3,
				MaxValueSum: 1 << 63,
			},
		},
		{
			name: "minValueSum overflows",
			bm: BallotMode{
				NumFields:   3,
				GroupSize:   3,
				MinValueSum: 1 << 63,
			},
		},
	}

	for _, tc := range cases {
		if _, err := tc.bm.Pack(); err == nil {
			t.Fatalf("expected error for %s", tc.name)
		}
	}
}

func assertField(t *testing.T, packed *big.Int, offset, size uint, want uint64) {
	t.Helper()
	mask := new(big.Int).Lsh(big.NewInt(1), size)
	mask.Sub(mask, big.NewInt(1))
	value := new(big.Int).Rsh(new(big.Int).Set(packed), offset)
	value.And(value, mask)
	if !value.IsUint64() {
		t.Fatalf("field at offset %d not uint64", offset)
	}
	if got := value.Uint64(); got != want {
		t.Fatalf("field at offset %d got %d want %d", offset, got, want)
	}
}
