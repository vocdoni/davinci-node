package main

import "testing"

func TestShouldRunSetup(t *testing.T) {
	t.Parallel()

	if shouldRunSetup("same", "same", false) {
		t.Fatalf("expected no setup when compiled hash matches expected hash")
	}

	if !shouldRunSetup("compiled", "expected", false) {
		t.Fatalf("expected setup when compiled hash differs from expected hash")
	}

	if !shouldRunSetup("same", "same", true) {
		t.Fatalf("expected setup when force is enabled")
	}
}
