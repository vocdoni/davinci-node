// Package bn254 implements the BN254 elliptic curve operations.
// It provides a wrapper around the gnark-crypto implementation to conform to the curve.Point interface.
package bn254

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sync"

	"github.com/fxamacker/cbor/v2"
	curve "github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/types"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
)

// CurveType is the identifier for the BN254 curve implementation
const CurveType = "bn254"

// Generator is the base generator point for the BN254 curve in Jacobian coordinates
var Generator bn254.G1Jac

func init() {
	Generator.X.SetOne()
	Generator.Y.SetUint64(2)
	Generator.Z.SetOne()
}

// G1 is the affine representation of a G1 group element.
type G1 struct {
	inner *bn254.G1Affine
	lock  sync.Mutex
}

// New creates a new G1 point (identity element by default)
func (g *G1) New() curve.Point {
	return &G1{inner: new(bn254.G1Affine)}
}

// Order returns the order of the BN254 curve group
func (g *G1) Order() *big.Int {
	return fr.Modulus()
}

// Add computes the addition of two curve points and stores the result in the receiver
func (g *G1) Add(a, b curve.Point) {
	temp := new(bn254.G1Affine)
	temp.Add(a.(*G1).inner, b.(*G1).inner)
	*g.inner = *temp
}

// SafeAdd performs thread-safe addition of two curve points
func (g *G1) SafeAdd(a, b curve.Point) {
	g.lock.Lock()
	defer g.lock.Unlock()
	g.inner.Add(a.(*G1).inner, b.(*G1).inner)
}

// ScalarMult computes the scalar multiplication of a point and stores the result in the receiver
func (g *G1) ScalarMult(a curve.Point, scalar *big.Int) {
	temp := new(bn254.G1Affine)
	temp.ScalarMultiplication(a.(*G1).inner, scalar)
	*g.inner = *temp
}

// ScalarBaseMult computes the scalar multiplication of the base point and stores the result in the receiver
func (g *G1) ScalarBaseMult(scalar *big.Int) {
	g.inner.ScalarMultiplicationBase(scalar)
}

// Marshal serializes the point to a byte slice
func (g *G1) Marshal() []byte {
	return g.inner.Marshal()
}

// Unmarshal deserializes a point from a byte slice
func (g *G1) Unmarshal(buf []byte) error {
	_, err := g.inner.SetBytes(buf)
	return err
}

// MarshalJSON serializes the elliptic curve element into a JSON byte slice
func (g *G1) MarshalJSON() ([]byte, error) {
	x := types.BigInt(*g.inner.X.BigInt(new(big.Int)))
	y := types.BigInt(*g.inner.Y.BigInt(new(big.Int)))
	return json.Marshal([]types.BigInt{x, y})
}

// UnmarshalJSON deserializes the elliptic curve element from a JSON byte slice
func (g *G1) UnmarshalJSON(buf []byte) error {
	if g.inner == nil {
		g.inner = new(bn254.G1Affine)
	}
	var coords []types.BigInt
	if err := json.Unmarshal(buf, &coords); err != nil {
		return err
	}
	if len(coords) != 2 {
		return fmt.Errorf("expected 2 coordinates, got %d", len(coords))
	}
	g.inner.X.SetBigInt(coords[0].MathBigInt())
	g.inner.Y.SetBigInt(coords[1].MathBigInt())
	return nil
}

// MarshalCBOR serializes the elliptic curve element into a CBOR byte slice
func (g *G1) MarshalCBOR() ([]byte, error) {
	x := g.inner.X.BigInt(new(big.Int))
	y := g.inner.Y.BigInt(new(big.Int))
	return cbor.Marshal([]*big.Int{x, y})
}

// UnmarshalCBOR deserializes the elliptic curve element from a CBOR byte slice
func (g *G1) UnmarshalCBOR(buf []byte) error {
	if g.inner == nil {
		g.inner = new(bn254.G1Affine)
	}
	var coords []*big.Int
	if err := cbor.Unmarshal(buf, &coords); err != nil {
		return err
	}
	if len(coords) != 2 {
		return fmt.Errorf("expected 2 coordinates, got %d", len(coords))
	}
	g.inner.X.SetBigInt(coords[0])
	g.inner.Y.SetBigInt(coords[1])
	return nil
}

// Equal checks if two curve points are equal
func (g *G1) Equal(a curve.Point) bool {
	return g.inner.Equal(a.(*G1).inner)
}

// Neg computes the negation of a curve point and stores the result in the receiver
func (g *G1) Neg(a curve.Point) {
	g.inner.Neg(a.(*G1).inner)
}

// SetZero sets the point to the identity element (zero)
func (g *G1) SetZero() {
	g.inner.X.SetZero()
	g.inner.Y.SetZero()
}

// Set copies the value from another curve point
func (g *G1) Set(a curve.Point) {
	g.inner.X.Set(&a.(*G1).inner.X)
	g.inner.Y.Set(&a.(*G1).inner.Y)
}

// SetGenerator sets the point to the base generator of the curve
func (g *G1) SetGenerator() {
	g.inner.FromJacobian(&Generator)
}

// String returns a string representation of the point
func (g *G1) String() string {
	bytes := g.Marshal()
	return fmt.Sprintf("%x", bytes)
}

// Point returns the x and y coordinates of the point
func (g *G1) Point() (*big.Int, *big.Int) {
	return g.inner.X.BigInt(new(big.Int)), g.inner.Y.BigInt(new(big.Int))
}

// SetPoint sets the point to the given x and y coordinates and returns the point
func (g *G1) SetPoint(x, y *big.Int) curve.Point {
	g = &G1{inner: new(bn254.G1Affine)}
	g.inner.X.SetBigInt(x)
	g.inner.Y.SetBigInt(y)
	return g
}

// Type returns the curve type identifier
func (g *G1) Type() string {
	return CurveType
}
