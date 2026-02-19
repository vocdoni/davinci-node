// Package format provides helper functions to transform points (x, y)
// from TwistedEdwards to Reduced TwistedEdwards and vice versa. These functions
// are required because Gnark uses the Reduced TwistedEdwards formula while
// Iden3 uses the standard TwistedEdwards formula.
// See https://github.com/bellesmarta/baby_jubjub for more information.
package format

import (
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
)

var (
	scalingFactor, _ = new(big.Int).SetString("6360561867910373094066688120553762416144456282423235903351243436111059670888", 10)
	negScalingFactor fr.Element
	negScalingInv    fr.Element
	negScalingBig    = new(big.Int)
	negScalingInvBig = new(big.Int)
)

func init() {
	var f fr.Element
	f.SetBigInt(scalingFactor)
	negScalingFactor.Neg(&f)
	negScalingInv.Inverse(&negScalingFactor)
	negScalingFactor.BigInt(negScalingBig)
	negScalingInv.BigInt(negScalingInvBig)
}

// FromRTEtoTE converts a point from Reduced TwistedEdwards to TwistedEdwards
// coordinates (from Gnark to Iden3). It applies the transformation:
//
//	x = x'/(-f)
//	y' = y
func FromRTEtoTE(x, y *big.Int) (*big.Int, *big.Int) {
	xTE := new(fr.Element)
	xTE.SetBigInt(x)
	// Step 4: Multiply g.inner.X by negFInv to get xTE
	xRTE := new(fr.Element)
	xRTE.Mul(xTE, &negScalingInv) // xTE = g.inner.X * negFInv mod p

	// Step 5: Convert xTE and g.inner.Y to *big.Int
	xRTEBigInt := new(big.Int)
	xRTE.BigInt(xRTEBigInt)
	return xRTEBigInt, y // x = x' / (-f) & y' = y
}

// FromTEtoRTE converts a point from TwistedEdwards to Reduced TwistedEdwards
// coordinates (from Iden3 to Gnark). It applies the transformation:
//
//	x' = x*(-f)
//	y = y'
func FromTEtoRTE(x, y *big.Int) (*big.Int, *big.Int) {
	// multiply x by negF to get xTE
	xRTE := new(fr.Element).SetBigInt(x)
	xTE := new(fr.Element)
	xTE.Mul(xRTE, &negScalingFactor) // xTE = g.inner.X * -f mod p
	// convert xTE to *big.Int
	xTEBigInt := new(big.Int)
	xTE.BigInt(xTEBigInt)
	return xTEBigInt, y // x' = x * (-f) & y = y'
}
