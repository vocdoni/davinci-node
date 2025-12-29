package blobs

import (
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

func TestBarycentricEvaluationBasic(t *testing.T) {
	// Create blob with direct values (these ARE the polynomial evaluations)
	blob := &types.Blob{}
	for i := 0; i < 20; i++ {
		big.NewInt(int64(i + 1)).FillBytes(blob[i*32 : (i+1)*32])
	}

	// Compute commitment first, then evaluation point z
	commitment, err := blob.ComputeCommitment()
	qt.Assert(t, err, qt.IsNil, qt.Commentf("ComputeCommitment should not return an error"))

	processID := util.RandomBytes(31)
	rootHashBefore := util.RandomBytes(31)

	z, err := ComputeEvaluationPoint(new(big.Int).SetBytes(processID), new(big.Int).SetBytes(rootHashBefore), commitment)
	qt.Assert(t, err, qt.IsNil, qt.Commentf("ComputeEvaluationPoint should not return an error"))

	// Ground truth from the KZG precompile
	_, claim, err := blob.ComputeProof(z)
	qt.Assert(t, err, qt.IsNil)

	// Evaluate using the barycentric formula
	got, err := EvaluateBarycentricNative(blob, z, false)
	qt.Assert(t, err, qt.IsNil, qt.Commentf("EvaluateBlobBarycentric should not return an error"))

	// Compare results
	qt.Assert(t, claim.Cmp(got), qt.Equals, 0, qt.Commentf("Expected and got values should match"))
}

func TestBarycentricEvaluationBlobData1(t *testing.T) {
	c := qt.New(t)
	blob, err := GetBlobData1()
	c.Assert(err, qt.IsNil)

	// Compute commitment first, then evaluation point z
	commitment, err := blob.ComputeCommitment()
	c.Assert(err, qt.IsNil)

	processID := util.RandomBytes(31)
	rootHashBefore := util.RandomBytes(31)
	z, err := ComputeEvaluationPoint(new(big.Int).SetBytes(processID), new(big.Int).SetBytes(rootHashBefore), commitment)
	qt.Assert(t, err, qt.IsNil, qt.Commentf("ComputeEvaluationPoint should not return an error"))

	// Ground truth from the KZG precompile
	_, claim, err := blob.ComputeProof(z)
	qt.Assert(t, err, qt.IsNil)

	// Evaluate
	got, err := EvaluateBarycentricNative(blob, z, false)
	c.Assert(err, qt.IsNil, qt.Commentf("EvaluateBlobBarycentric should not return an error"))
	qt.Assert(c, claim.Cmp(got), qt.Equals, 0, qt.Commentf("Expected and got values should match"))
}

func TestBarycentricEvaluationBlobData2(t *testing.T) {
	c := qt.New(t)
	blob, err := GetBlobData2()
	c.Assert(err, qt.IsNil)

	// Compute commitment first, then evaluation point z
	commitment, err := blob.ComputeCommitment()
	c.Assert(err, qt.IsNil)

	processID := util.RandomBytes(31)
	rootHashBefore := util.RandomBytes(31)
	z, err := ComputeEvaluationPoint(new(big.Int).SetBytes(processID), new(big.Int).SetBytes(rootHashBefore), commitment)
	qt.Assert(t, err, qt.IsNil, qt.Commentf("ComputeEvaluationPoint should not return an error"))

	// Ground truth from the KZG precompile
	_, claim, err := blob.ComputeProof(z)
	qt.Assert(t, err, qt.IsNil)

	// Evaluate
	got, err := EvaluateBarycentricNative(blob, z, false) // Enable debug for better output
	c.Assert(err, qt.IsNil, qt.Commentf("EvaluateBlobBarycentric should not return an error"))
	qt.Assert(c, claim.Cmp(got), qt.Equals, 0, qt.Commentf("Expected and got values should match"))
}
