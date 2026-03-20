package circuits

import (
	"fmt"
	"math/big"

	ecc_tweds "github.com/consensys/gnark-crypto/ecc/twistededwards"
	"github.com/consensys/gnark/frontend"
	tweds "github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/vocdoni/davinci-node/types"
)

// FrontendError function is an in-circuit function to print an error message
// and an error trace, making the circuit fail.
func FrontendError(api frontend.API, msg string, trace error) {
	api.Println("in-circuit error: " + msg)
	api.Println(fmt.Sprintf("%s: %s", msg, trace.Error()))
	api.AssertIsEqual(1, 0)
}

// AssertIsEqualIf fails if condition is true and i1 != i2.
// If condition is false, the check is skipped.
func AssertIsEqualIf(api frontend.API, condition, i1, i2 frontend.Variable) {
	api.AssertIsEqual(api.Select(condition, i1, i2), i2)
}

// AssertTrueIf fails if condition is true and mustBeTrue is not (mustBeTrue != 1).
// If condition is false, the check is skipped.
func AssertTrueIf(api frontend.API, condition, mustBeTrue frontend.Variable) {
	AssertIsEqualIf(api, condition, mustBeTrue, 1)
}

// BigIntArrayToN pads the big.Int array to n elements, if needed, with zeros.
func BigIntArrayToN(arr []*big.Int, n int) []*big.Int {
	bigArr := make([]*big.Int, n)
	for i := range n {
		if i < len(arr) {
			bigArr[i] = arr[i]
		} else {
			bigArr[i] = big.NewInt(0)
		}
	}
	return bigArr
}

// BigIntArrayToNInternal pads the types.BigInt array to n elements, if needed,
// with zeros.
func BigIntArrayToNInternal(arr []*big.Int, n int) []*types.BigInt {
	bigArr := make([]*types.BigInt, n)
	for i := range n {
		if i < len(arr) {
			bigArr[i] = new(types.BigInt).SetBigInt(arr[i])
		} else {
			bigArr[i] = types.NewInt(0)
		}
	}
	return bigArr
}

// AssertValidBJJPoint constrains a BabyJubJub point to be on-curve.
func AssertValidBJJPoint(api frontend.API, point tweds.Point) {
	curve, err := tweds.NewEdCurve(api, ecc_tweds.BN254)
	if err != nil {
		FrontendError(api, "failed to initialize babyjubjub curve", err)
		return
	}
	curve.AssertIsOnCurve(point)
}
