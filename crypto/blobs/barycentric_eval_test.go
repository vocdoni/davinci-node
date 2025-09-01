package blobs

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"testing"

	goethkzg "github.com/crate-crypto/go-eth-kzg"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/util"
)

func TestBarycentricEvalGo(t *testing.T) {
	// Create blob with direct values (these ARE the polynomial evaluations)
	blob := &goethkzg.Blob{}
	for i := 0; i < 20; i++ {
		big.NewInt(int64(i + 1)).FillBytes(blob[i*32 : (i+1)*32])
	}

	// Evaluation point z
	processID := util.RandomBytes(31)
	rootHashBefore := util.RandomBytes(31)

	z, err := ComputeEvaluationPoint(new(big.Int).SetBytes(processID), new(big.Int).SetBytes(rootHashBefore), blob)
	qt.Assert(t, err, qt.IsNil, qt.Commentf("ComputeEvaluationPoint should not return an error"))

	// Ground truth from the KZG precompile
	_, claim, _ := ComputeProof(blob, z)
	want := new(big.Int).SetBytes(claim[:])

	// Evaluate using the barycentric formula
	got, err := EvaluateBlobBarycentricNativeGo(blob, z, false)
	qt.Assert(t, err, qt.IsNil, qt.Commentf("EvaluateBlobBarycentric should not return an error"))

	// Compare results
	qt.Assert(t, want.Cmp(got), qt.Equals, 0, qt.Commentf("Expected and got values should match"))
}

func TestBarycentricEvalGoBlobData1(t *testing.T) {
	c := qt.New(t)
	data, err := os.ReadFile("testdata/blobdata1.txt")
	if err != nil {
		// skip test
		t.Skipf("blobdata1.txt not found, skipping test: %v", err)
	}
	blob, err := hexStrToBlob(string(data))
	c.Assert(err, qt.IsNil)

	// Evaluation point z
	processID := util.RandomBytes(31)
	rootHashBefore := util.RandomBytes(31)
	z, err := ComputeEvaluationPoint(new(big.Int).SetBytes(processID), new(big.Int).SetBytes(rootHashBefore), blob)
	qt.Assert(t, err, qt.IsNil, qt.Commentf("ComputeEvaluationPoint should not return an error"))

	// Ground truth from the KZG precompile
	_, claim, _ := ComputeProof(blob, z)
	want := new(big.Int).SetBytes(claim[:])

	// Evaluate
	got, err := EvaluateBlobBarycentricNativeGo(blob, z, false)
	c.Assert(err, qt.IsNil, qt.Commentf("EvaluateBlobBarycentric should not return an error"))
	qt.Assert(c, want.Cmp(got), qt.Equals, 0, qt.Commentf("Expected and got values should match"))
}

func TestBarycentricEvalGoBlobData2(t *testing.T) {
	c := qt.New(t)
	data, err := os.ReadFile("testdata/blobdata2.txt")
	if err != nil {
		// skip test
		t.Skipf("blobdata2.txt not found, skipping test: %v", err)
	}
	blob, err := hexStrToBlob(string(data))
	c.Assert(err, qt.IsNil)

	// Evaluation point z
	processID := util.RandomBytes(31)
	rootHashBefore := util.RandomBytes(31)
	z, err := ComputeEvaluationPoint(new(big.Int).SetBytes(processID), new(big.Int).SetBytes(rootHashBefore), blob)
	qt.Assert(t, err, qt.IsNil, qt.Commentf("ComputeEvaluationPoint should not return an error"))

	// Ground truth from the KZG precompile
	_, claim, _ := ComputeProof(blob, z)
	want := new(big.Int).SetBytes(claim[:])

	// Evaluate
	got, err := EvaluateBlobBarycentricNativeGo(blob, z, false) // Enable debug for better output
	c.Assert(err, qt.IsNil, qt.Commentf("EvaluateBlobBarycentric should not return an error"))
	qt.Assert(c, want.Cmp(got), qt.Equals, 0, qt.Commentf("Expected and got values should match"))
}

func hexStrToBlob(hexStr string) (*goethkzg.Blob, error) {
	var blob goethkzg.Blob
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
