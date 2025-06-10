package bjj

import (
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/vocdoni/davinci-node/crypto/ecc"
	bjjIden3 "github.com/vocdoni/davinci-node/crypto/ecc/bjj_iden3"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
)

func PointFromRTEtoTE(p ecc.Point) ecc.Point {
	return p.New().SetPoint(format.FromRTEtoTE(p.Point()))
}

func PointFromTEtoRTE(p ecc.Point) ecc.Point {
	return p.New().SetPoint(format.FromTEtoRTE(p.Point()))
}

// Helper function to generate a non-base point
func generateNonBasePoint() (ecc.Point, ecc.Point) {
	scalar := big.NewInt(123456789) // Fixed scalar for reproducibility
	bjjPoint := New()
	iden3Point := bjjIden3.New()

	// Multiply the base point by the scalar to get a new point
	bjjPoint.ScalarBaseMult(scalar)
	iden3Point.ScalarBaseMult(scalar)

	return bjjPoint, iden3Point
}

func TestSetGenerator(t *testing.T) {
	c := qt.New(t)
	bjjPoint := New()
	iden3Point := bjjIden3.New()

	bjjPoint.SetGenerator()
	iden3Point.SetGenerator()
	c.Assert(PointFromRTEtoTE(bjjPoint).String(), qt.Equals, iden3Point.String())
}

func TestOrder(t *testing.T) {
	c := qt.New(t)
	bjjPoint := New()
	iden3Point := bjjIden3.New()

	c.Assert(bjjPoint.Order().String(), qt.Equals, iden3Point.Order().String())
}

func TestSetZero(t *testing.T) {
	c := qt.New(t)
	bjjPoint := New()
	iden3Point := bjjIden3.New()

	bjjPoint.SetZero()
	iden3Point.SetZero()

	c.Assert(PointFromRTEtoTE(bjjPoint).String(), qt.Equals, iden3Point.String())
}

func TestScalarBaseMult(t *testing.T) {
	c := qt.New(t)
	scalar := big.NewInt(42)
	bjjPoint := New()
	iden3Point := bjjIden3.New()

	bjjPoint.ScalarBaseMult(scalar)
	iden3Point.ScalarBaseMult(scalar)

	c.Assert(PointFromRTEtoTE(bjjPoint).String(), qt.Equals, iden3Point.String())
}

func TestScalarMult(t *testing.T) {
	c := qt.New(t)
	scalar := big.NewInt(88)
	// Generate a non-base point
	bjjPoint, iden3Point := generateNonBasePoint()

	bjjPoint.ScalarMult(bjjPoint, scalar)
	iden3Point.ScalarMult(iden3Point, scalar)

	c.Assert(PointFromRTEtoTE(bjjPoint).String(), qt.Equals, iden3Point.String())
}

func TestAdd(t *testing.T) {
	c := qt.New(t)
	// Generate two non-base points
	bjjPointA := New()
	bjjPointB := New()
	iden3PointA := bjjIden3.New()
	iden3PointB := bjjIden3.New()

	// Use fixed scalars to ensure consistent points
	scalarA := big.NewInt(123456789)
	scalarB := big.NewInt(987654321)

	bjjPointA.ScalarBaseMult(scalarA)
	iden3PointA.ScalarBaseMult(scalarA)

	bjjPointB.ScalarBaseMult(scalarB)
	iden3PointB.ScalarBaseMult(scalarB)

	bjjPointA.Add(bjjPointA, bjjPointB)
	iden3PointA.Add(iden3PointA, iden3PointB)

	c.Assert(PointFromRTEtoTE(bjjPointA).String(), qt.Equals, iden3PointA.String())
}

func TestNeg(t *testing.T) {
	c := qt.New(t)
	// Generate a non-base point
	bjjPoint, iden3Point := generateNonBasePoint()

	bjjPoint.Neg(bjjPoint)
	iden3Point.Neg(iden3Point)

	c.Assert(PointFromRTEtoTE(bjjPoint).String(), qt.Equals, iden3Point.String())
}

func TestDouble(t *testing.T) {
	c := qt.New(t)
	// Generate a non-base point
	bjjPoint, iden3Point := generateNonBasePoint()

	// Double the point: 2P = P + P
	bjjPointDbl := New()
	iden3PointDbl := bjjIden3.New()

	bjjPointDbl.Add(bjjPoint, bjjPoint)
	iden3PointDbl.Add(iden3Point, iden3Point)

	c.Assert(PointFromRTEtoTE(bjjPointDbl).String(), qt.Equals, iden3PointDbl.String())
}

func TestEqual(t *testing.T) {
	c := qt.New(t)
	// Generate a non-base point
	bjjPoint1, iden3Point1 := generateNonBasePoint()

	// Clone the points
	bjjPoint2 := New()
	iden3Point2 := bjjIden3.New()
	bjjPoint2.Set(bjjPoint1)
	iden3Point2.Set(iden3Point1)

	c.Assert(bjjPoint1.Equal(bjjPoint2), qt.IsTrue)
	c.Assert(iden3Point1.Equal(iden3Point2), qt.IsTrue)

	// Modify one point
	bjjPoint2.ScalarMult(bjjPoint2, big.NewInt(2))
	iden3Point2.ScalarMult(iden3Point2, big.NewInt(2))

	c.Assert(bjjPoint1.Equal(bjjPoint2), qt.IsFalse)
	c.Assert(iden3Point1.Equal(iden3Point2), qt.IsFalse)
}
