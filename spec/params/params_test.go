package params

import "testing"

func TestVoteIDNamespaceConstants(t *testing.T) {
	if VoteIDMin != 0x8000_0000_0000_0000 {
		t.Fatalf("VoteIDMin mismatch: got %x", VoteIDMin)
	}
	if VoteIDMax != 0xFFFF_FFFF_FFFF_FFFF {
		t.Fatalf("VoteIDMax mismatch: got %x", VoteIDMax)
	}
}
