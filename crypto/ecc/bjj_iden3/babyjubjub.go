// Package bjj implements the BabyJubJub elliptic curve operations using the iden3 library.
// It provides a wrapper around the iden3 implementation to conform to the curve.Point interface.
package bjj

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sync"

	"github.com/fxamacker/cbor/v2"
	babyjubjub "github.com/iden3/go-iden3-crypto/babyjub"

	curve "github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/types"
)

// CurveType is the identifier for the BabyJubJub curve implementation using iden3 library
const CurveType = "bjj_iden3"

// BJJ is the affine representation of the BabyJubJub group element.
type BJJ struct {
	inner *babyjubjub.Point
	lock  sync.Mutex
}

// New creates a new BJJ point (identity element by default).
func New() curve.Point {
	return &BJJ{inner: babyjubjub.NewPoint()}
}

// New creates a new BJJ point (identity element by default)
func (g *BJJ) New() curve.Point {
	return &BJJ{inner: babyjubjub.NewPoint()}
}

// Order returns the order of the BabyJubJub curve subgroup
func (g *BJJ) Order() *big.Int {
	return babyjubjub.SubOrder
}

// Add computes the addition of two curve points and stores the result in the receiver
func (g *BJJ) Add(a, b curve.Point) {
	g.inner = g.inner.Projective().Add(a.(*BJJ).inner.Projective(), b.(*BJJ).inner.Projective()).Affine()
}

// SafeAdd performs thread-safe addition of two curve points
func (g *BJJ) SafeAdd(a, b curve.Point) {
	g.lock.Lock()
	defer g.lock.Unlock()
	g.Add(a, b)
}

// ScalarMult computes the scalar multiplication of a point and stores the result in the receiver
func (g *BJJ) ScalarMult(a curve.Point, scalar *big.Int) {
	g.inner = g.inner.Mul(scalar, a.(*BJJ).inner)
}

// ScalarBaseMult computes the scalar multiplication of the base point and stores the result in the receiver
func (g *BJJ) ScalarBaseMult(scalar *big.Int) {
	g.inner = g.inner.Mul(scalar, babyjubjub.B8)
}

// Marshal compresses and serializes the point to a byte slice
func (g *BJJ) Marshal() []byte {
	b := g.inner.Compress()
	return b[:]
}

// Unmarshal deserializes and decompresses a point from a byte slice
func (g *BJJ) Unmarshal(buf []byte) error {
	b32 := [32]byte{}
	copy(b32[:], buf)
	_, err := g.inner.Decompress(b32)
	return err
}

// MarshalJSON serializes the elliptic curve element into a JSON byte slice.
func (g *BJJ) MarshalJSON() ([]byte, error) {
	return json.Marshal([]types.BigInt{types.BigInt(*g.inner.X), types.BigInt(*g.inner.Y)})
}

// UnmarshalJSON deserializes the elliptic curve element from a JSON byte slice.
func (g *BJJ) UnmarshalJSON(buf []byte) error {
	if g.inner == nil {
		g.inner = babyjubjub.NewPoint()
	}
	var coords []types.BigInt
	if err := json.Unmarshal(buf, &coords); err != nil {
		return err
	}
	if len(coords) != 2 {
		return fmt.Errorf("expected 2 coordinates, got %d", len(coords))
	}
	g.inner.X = coords[0].MathBigInt()
	g.inner.Y = coords[1].MathBigInt()
	return nil
}

// MarshalCBOR serializes the elliptic curve element into a CBOR byte slice
func (g *BJJ) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal([]*big.Int{g.inner.X, g.inner.Y})
}

// UnmarshalCBOR deserializes the elliptic curve element from a CBOR byte slice
func (g *BJJ) UnmarshalCBOR(buf []byte) error {
	if g.inner == nil {
		g.inner = babyjubjub.NewPoint()
	}
	var coords []*big.Int
	if err := cbor.Unmarshal(buf, &coords); err != nil {
		return err
	}
	if len(coords) != 2 {
		return fmt.Errorf("expected 2 coordinates, got %d", len(coords))
	}
	g.inner.X = coords[0]
	g.inner.Y = coords[1]
	return nil
}

// Equal checks if two curve points are equal
func (g *BJJ) Equal(a curve.Point) bool {
	return g.inner.X.Cmp(a.(*BJJ).inner.X) == 0 && g.inner.Y.Cmp(a.(*BJJ).inner.Y) == 0
}

// Neg computes the negation of a curve point and stores the result in the receiver
func (g *BJJ) Neg(a curve.Point) {
	// Set the receiver to the negation of the input point
	g.Set(a)
	proj := g.inner.Projective()
	proj.X = proj.X.Neg(proj.X)
	g.inner.X = g.inner.X.Set(proj.Affine().X)
	// g.inner.X = g.inner.X.Neg(g.inner.X) // Negate the x-coordinate
	// g.inner.X = g.inner.X.Mod(g.inner.X, constants.Q)
}

// SetZero sets the point to the identity element (zero)
func (g *BJJ) SetZero() {
	p := g.inner.Projective()
	p.X.SetZero() // Set X to 0
	p.Y.SetOne()  // Set Y to 1
	p.Z.SetOne()  // Set Z to 1
	g.inner = p.Affine()
}

// Set copies the value from another curve point
func (g *BJJ) Set(a curve.Point) {
	g.inner.X = g.inner.X.Set(a.(*BJJ).inner.X)
	g.inner.Y = g.inner.Y.Set(a.(*BJJ).inner.Y)
}

// SetGenerator sets the point to the base generator of the curve
func (g *BJJ) SetGenerator() {
	gen := babyjubjub.B8
	g.inner.X = g.inner.X.Set(gen.X)
	g.inner.Y = g.inner.Y.Set(gen.Y)
}

// String returns a string representation of the point
func (g *BJJ) String() string {
	// bytes := g.Marshal()
	// return fmt.Sprintf("%x", bytes)
	return fmt.Sprintf("%s,%s", g.inner.X.String(), g.inner.Y.String())
}

// Point returns the x and y coordinates of the point
func (g *BJJ) Point() (*big.Int, *big.Int) {
	return g.inner.X, g.inner.Y
}

// BigInts returns the x and y coordinates of the point as a slice of big.Int
func (g *BJJ) BigInts() []*big.Int {
	x, y := g.Point()
	return []*big.Int{x, y}
}

// SetPoint sets the point to the given x and y coordinates and returns the point
func (g *BJJ) SetPoint(x, y *big.Int) curve.Point {
	g = &BJJ{inner: babyjubjub.NewPoint()}
	g.inner.X = g.inner.X.Set(x)
	g.inner.Y = g.inner.Y.Set(y)
	return g
}

// Type returns the curve type identifier
func (g *BJJ) Type() string {
	return CurveType
}
