package circuits

import (
	"fmt"
	"testing"

	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/spec/params"
)

func TestNewEmulatedBallot(t *testing.T) {
	c := qt.New(t)

	ballot := NewEmulatedBallot[sw_bn254.ScalarField]()
	zero := emulated.ValueOf[sw_bn254.ScalarField](0)
	one := emulated.ValueOf[sw_bn254.ScalarField](1)

	c.Assert(ballot, qt.Not(qt.IsNil))
	c.Assert(*ballot, qt.HasLen, params.FieldsPerBallot)
	for _, field := range *ballot {
		c.Assert(fmt.Sprint(field.C1.X.Limbs), qt.Equals, fmt.Sprint(zero.Limbs))
		c.Assert(fmt.Sprint(field.C1.Y.Limbs), qt.Equals, fmt.Sprint(one.Limbs))
		c.Assert(fmt.Sprint(field.C2.X.Limbs), qt.Equals, fmt.Sprint(zero.Limbs))
		c.Assert(fmt.Sprint(field.C2.Y.Limbs), qt.Equals, fmt.Sprint(one.Limbs))
	}
}
