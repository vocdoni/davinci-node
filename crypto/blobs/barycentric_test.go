package blobs

import (
	"math/big"
	"testing"

	goethkzg "github.com/crate-crypto/go-eth-kzg"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/util"
)

func TestBarycentricEvaluationBasic(t *testing.T) {
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
	got, err := EvaluateBarycentricNative(blob, z, false)
	qt.Assert(t, err, qt.IsNil, qt.Commentf("EvaluateBlobBarycentric should not return an error"))

	// Compare results
	qt.Assert(t, want.Cmp(got), qt.Equals, 0, qt.Commentf("Expected and got values should match"))
}

func TestBarycentricEvaluationBlobData1(t *testing.T) {
	c := qt.New(t)
	blob, err := GetBlobData1()
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
	got, err := EvaluateBarycentricNative(blob, z, false)
	c.Assert(err, qt.IsNil, qt.Commentf("EvaluateBlobBarycentric should not return an error"))
	qt.Assert(c, want.Cmp(got), qt.Equals, 0, qt.Commentf("Expected and got values should match"))
}

func TestBarycentricEvaluationBlobData2(t *testing.T) {
	c := qt.New(t)
	blob, err := GetBlobData2()
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
	got, err := EvaluateBarycentricNative(blob, z, false) // Enable debug for better output
	c.Assert(err, qt.IsNil, qt.Commentf("EvaluateBlobBarycentric should not return an error"))
	qt.Assert(c, want.Cmp(got), qt.Equals, 0, qt.Commentf("Expected and got values should match"))
}
