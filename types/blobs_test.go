package types

import (
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
)

func TestComputeProofRejectsOversizedPoint(t *testing.T) {
	c := qt.New(t)

	data := make([]byte, BlobLength)
	blob, err := NewBlobFromBytes(data)
	c.Assert(err, qt.IsNil)

	point := new(big.Int).Lsh(big.NewInt(1), 300)
	_, _, err = blob.ComputeProof(point)
	c.Assert(err, qt.Not(qt.IsNil))
}
