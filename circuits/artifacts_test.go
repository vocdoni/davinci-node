package circuits

import (
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	qt "github.com/frankban/quicktest"
)

func TestCircuitRuntime(t *testing.T) {
	artifacts := NewCircuitArtifacts("test", ecc.BN254, nil, nil, nil, nil, nil)
	qt.Assert(t, artifacts.Name(), qt.Equals, "test")
	qt.Assert(t, artifacts.Curve(), qt.Equals, ecc.BN254)

	runtime := NewCircuitRuntime("test", ecc.BN254, nil, nil, nil, nil, nil)
	qt.Assert(t, runtime.Name(), qt.Equals, "test")
	qt.Assert(t, runtime.Curve(), qt.Equals, ecc.BN254)
	qt.Assert(t, runtime.ProvingKey(), qt.IsNil)
}
