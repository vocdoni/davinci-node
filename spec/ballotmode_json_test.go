package spec

import (
	"encoding/json"
	"testing"
)

func TestBallotModeJSONTags(t *testing.T) {
	bm := BallotMode{
		NumFields:      2,
		GroupSize:      1,
		UniqueValues:   true,
		CostFromWeight: false,
		CostExponent:   3,
		MaxValue:       4,
		MinValue:       1,
		MaxValueSum:    10,
		MinValueSum:    2,
	}

	data, err := json.Marshal(bm)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	assertJSONFieldNumber(t, decoded, "numFields", 2)
	assertJSONFieldNumber(t, decoded, "groupSize", 1)
	assertJSONFieldBool(t, decoded, "uniqueValues", true)
	assertJSONFieldBool(t, decoded, "costFromWeight", false)
	assertJSONFieldNumber(t, decoded, "costExponent", 3)
	assertJSONFieldNumber(t, decoded, "maxValue", 4)
	assertJSONFieldNumber(t, decoded, "minValue", 1)
	assertJSONFieldNumber(t, decoded, "maxValueSum", 10)
	assertJSONFieldNumber(t, decoded, "minValueSum", 2)
}

func assertJSONFieldNumber(t *testing.T, decoded map[string]any, key string, want int) {
	t.Helper()
	value, ok := decoded[key]
	if !ok {
		t.Fatalf("missing json field %s", key)
	}
	got, ok := value.(float64)
	if !ok {
		t.Fatalf("json field %s not number: %T", key, value)
	}
	if int(got) != want {
		t.Fatalf("json field %s = %v want %d", key, got, want)
	}
}

func assertJSONFieldBool(t *testing.T, decoded map[string]any, key string, want bool) {
	t.Helper()
	value, ok := decoded[key]
	if !ok {
		t.Fatalf("missing json field %s", key)
	}
	got, ok := value.(bool)
	if !ok {
		t.Fatalf("json field %s not bool: %T", key, value)
	}
	if got != want {
		t.Fatalf("json field %s = %v want %v", key, got, want)
	}
}
