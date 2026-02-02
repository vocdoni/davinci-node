package format

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
)

// FromRTEtoTEVar converts a point from Reduced TwistedEdwards to TwistedEdwards
// coordinates using frontend variables.
func FromRTEtoTEVar(api frontend.API, x, y frontend.Variable) (frontend.Variable, frontend.Variable) {
	xTE := api.Mul(x, negScalingInvBig)
	return xTE, y
}

// FromEmulatedRTEtoTE converts a point from Reduced TwistedEdwards to TwistedEdwards
// coordinates using emulated BN254 elements.
func FromEmulatedRTEtoTE(
	api frontend.API,
	x, y emulated.Element[sw_bn254.ScalarField],
) (emulated.Element[sw_bn254.ScalarField], emulated.Element[sw_bn254.ScalarField], error) {
	field, err := emulated.NewField[sw_bn254.ScalarField](api)
	if err != nil {
		return emulated.Element[sw_bn254.ScalarField]{}, emulated.Element[sw_bn254.ScalarField]{}, err
	}
	negInv := emulated.ValueOf[sw_bn254.ScalarField](negScalingInvBig)
	xTE := field.Mul(&x, &negInv)
	return *xTE, y, nil
}
