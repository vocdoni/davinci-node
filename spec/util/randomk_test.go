package util

import "testing"

func TestRandomKNonNil(t *testing.T) {
	k, err := RandomK()
	if err != nil {
		t.Fatalf("RandomK: %v", err)
	}
	if k == nil {
		t.Fatalf("RandomK returned nil")
	}
}
