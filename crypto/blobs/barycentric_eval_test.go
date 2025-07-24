package blobs

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	qt "github.com/frankban/quicktest"
)

func TestBarycentricEvalGo(t *testing.T) {
	// Create blob with direct values (these ARE the polynomial evaluations)
	blob := &kzg4844.Blob{}
	for i := 0; i < 20; i++ {
		big.NewInt(int64(i + 1)).FillBytes(blob[i*32 : (i+1)*32])
	}

	// Use a simple fixed evaluation point instead of ComputeEvaluationPoint
	z := big.NewInt(12345)

	// Ground truth from the KZG precompile
	_, claim, _ := ComputeProof(blob, z)
	want := new(big.Int).SetBytes(claim[:])

	// Pure Go reproduction of the circuit algorithm
	got, err := EvaluateBlobBarycentric(blob, z, false) // Disable debug for cleaner output
	if err != nil {
		t.Fatal(err)
	}
	if want.Cmp(got) != 0 {
		t.Fatalf("mismatch:\nexpected %s\ngot      %s", want, got)
	}
}

func TestBarycentricEval4ElementsGo(t *testing.T) {
	// Create blob with direct values (these ARE the polynomial evaluations)
	blob := &kzg4844.Blob{}
	for i := 0; i < 4; i++ {
		big.NewInt(int64(i + 1)).FillBytes(blob[i*32 : (i+1)*32])
	}

	// Use a simple fixed evaluation point instead of ComputeEvaluationPoint
	z := big.NewInt(12345)

	// Ground truth from the KZG precompile
	_, claim, _ := ComputeProof(blob, z)
	want := new(big.Int).SetBytes(claim[:])

	// Pure Go reproduction of the circuit algorithm
	got, err := EvaluateBlobBarycentric(blob, z, false) // Disable debug for cleaner output
	if err != nil {
		t.Fatal(err)
	}
	if want.Cmp(got) != 0 {
		t.Fatalf("mismatch:\nexpected %s\ngot      %s", want, got)
	}
}
func TestBarycentricEval4SparseElementsGo(t *testing.T) {
	// Create blob with direct values (these ARE the polynomial evaluations)
	blob := &kzg4844.Blob{}
	for i := 1; i < 6; i++ {
		big.NewInt(int64(i + 1)).FillBytes(blob[i*32 : (i+1)*32])
	}

	// Use a simple fixed evaluation point instead of ComputeEvaluationPoint
	z := big.NewInt(12345)

	// Ground truth from the KZG precompile
	_, claim, _ := ComputeProof(blob, z)
	want := new(big.Int).SetBytes(claim[:])

	// Pure Go reproduction of the circuit algorithm
	got, err := EvaluateBlobBarycentric(blob, z, false) // Disable debug for cleaner output
	if err != nil {
		t.Fatal(err)
	}
	if want.Cmp(got) != 0 {
		t.Fatalf("mismatch:\nexpected %s\ngot      %s", want, got)
	}
}

func TestConsensusBlobEvalCircuit(t *testing.T) {
	c := qt.New(t)
	data, err := os.ReadFile("blobdata.txt")
	c.Assert(err, qt.IsNil)
	t.Logf("Read %d bytes from blobdata.txt", len(data))
	blob, err := hexStrToBlob(string(data))
	c.Assert(err, qt.IsNil)
	// Check blob length
	c.Assert(len(blob), qt.Equals, 4096*32)

	// Use a simple fixed evaluation point instead of ComputeEvaluationPoint
	z := big.NewInt(12345)

	// Ground truth from the KZG precompile
	_, claim, _ := ComputeProof(blob, z)
	want := new(big.Int).SetBytes(claim[:])

	// Pure Go reproduction of the circuit algorithm
	got, err := EvaluateBlobBarycentric(blob, z, false) // Disable debug for cleaner output
	if err != nil {
		t.Fatal(err)
	}
	if want.Cmp(got) != 0 {
		t.Fatalf("mismatch:\nexpected %s\ngot      %s", want, got)
	}
}

func hexStrToBlob(hexStr string) (*kzg4844.Blob, error) {
	var blob kzg4844.Blob
	byts, err := hexStrToBytes(hexStr)
	if err != nil {
		return nil, err
	}

	if len(blob) != len(byts) {
		return nil, fmt.Errorf("blob does not have the correct length, %d ", len(byts))
	}
	copy(blob[:], byts)
	return &blob, nil
}

func hexStrToBytes(hexStr string) ([]byte, error) {
	return hex.DecodeString(hexStr)
}
