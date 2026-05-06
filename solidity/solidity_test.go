package solidity

import (
	"math/big"
	"testing"
)

func TestGroth16CommitmentProofMarshalSolidity(t *testing.T) {
	proof := Groth16CommitmentProof{
		Proof: SolidityProof{
			Ar: [2]*big.Int{big.NewInt(1), big.NewInt(2)},
			Bs: [2][2]*big.Int{
				{big.NewInt(3), big.NewInt(4)},
				{big.NewInt(5), big.NewInt(6)},
			},
			Krs: [2]*big.Int{big.NewInt(7), big.NewInt(8)},
		},
		Commitments:   [2]*big.Int{big.NewInt(9), big.NewInt(10)},
		CommitmentPok: [2]*big.Int{big.NewInt(11), big.NewInt(12)},
	}

	got := proof.MarshalSolidity()
	if len(got) != 384 {
		t.Fatalf("MarshalSolidity length mismatch: got %d want 384", len(got))
	}

	abiGot, err := proof.ABIEncode()
	if err != nil {
		t.Fatalf("ABIEncode error: %v", err)
	}
	if string(abiGot) != string(got) {
		t.Fatalf("ABIEncode mismatch")
	}
}
