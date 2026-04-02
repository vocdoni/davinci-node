package voteverifiertest

import (
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
)

func TestVerifyVoteCircuitRejectsOffCurvePublicKey(t *testing.T) {
	c := qt.New(t)

	placeholder, err := voteverifier.DummyPlaceholder()
	c.Assert(err, qt.IsNil)
	assignment, err := voteverifier.DummyAssignment()
	c.Assert(err, qt.IsNil)

	assignment.PublicKey.X = emulated.ValueOf[emulated.Secp256k1Fp](1)
	assignment.PublicKey.Y = emulated.ValueOf[emulated.Secp256k1Fp](1)

	assert := test.NewAssert(t)
	assert.SolvingFailed(placeholder, assignment,
		test.WithCurves(ecc.BLS12_377),
		test.WithBackends(backend.GROTH16))
}
