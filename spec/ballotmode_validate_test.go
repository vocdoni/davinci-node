package spec

import (
	"testing"

	"github.com/vocdoni/davinci-node/spec/params"
)

func TestBallotModeValidate(t *testing.T) {
	valid := BallotMode{
		NumFields:   uint8(params.FieldsPerBallot),
		GroupSize:   1,
		MaxValue:    10,
		MinValue:    0,
		MaxValueSum: 20,
		MinValueSum: 0,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid ballot mode: %v", err)
	}

	tooManyFields := valid
	tooManyFields.NumFields = uint8(params.FieldsPerBallot + 1)
	if err := tooManyFields.Validate(); err == nil {
		t.Fatalf("expected error for numFields exceeding max")
	}

	groupTooLarge := valid
	groupTooLarge.GroupSize = valid.NumFields + 1
	if err := groupTooLarge.Validate(); err == nil {
		t.Fatalf("expected error for groupSize exceeding numFields")
	}

	minGreater := valid
	minGreater.MinValue = 20
	minGreater.MaxValue = 10
	if err := minGreater.Validate(); err == nil {
		t.Fatalf("expected error for minValue > maxValue")
	}

	sumMinGreater := valid
	sumMinGreater.MinValueSum = 30
	sumMinGreater.MaxValueSum = 10
	if err := sumMinGreater.Validate(); err == nil {
		t.Fatalf("expected error for minValueSum > maxValueSum")
	}
}
