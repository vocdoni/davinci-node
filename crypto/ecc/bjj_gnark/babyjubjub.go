package bjj

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sync"

	babyjubjub "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/fxamacker/cbor/v2"
	curve "github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/types"
)

const CurveType = "bjj_gnark"

var Params babyjubjub.CurveParams

// BJJ is the affine representation of the BabyJubJub group element.
type BJJ struct {
	inner *babyjubjub.PointAffine
	lock  sync.Mutex
}

// Scaling factor f (as big.Int)
var scalingFactor *big.Int

func init() {
	Params = babyjubjub.GetEdwardsCurve()
	scalingFactor = new(big.Int)
	scalingFactor.SetString("6360561867910373094066688120553762416144456282423235903351243436111059670888", 10)
}

// New creates a new BJJ point (identity element by default).
func New() curve.Point {
	return &BJJ{inner: new(babyjubjub.PointAffine)}
}

// New creates a new BJJ point (identity element by default).
func (g *BJJ) New() curve.Point {
	p := &BJJ{inner: new(babyjubjub.PointAffine)}
	p.SetZero()
	return p
}

// Order returns the order of the BabyJubJub curve subgroup.
func (g *BJJ) Order() *big.Int {
	return new(big.Int).Set(&Params.Order)
}

// Add performs the addition of two points and stores the result in g.
func (g *BJJ) Add(a, b curve.Point) {
	g.inner.Add(a.(*BJJ).inner, b.(*BJJ).inner)
}

// SafeAdd performs the addition of two points with a lock.
func (g *BJJ) SafeAdd(a, b curve.Point) {
	g.lock.Lock()
	defer g.lock.Unlock()
	g.Add(a, b)
}

// ScalarMult performs scalar multiplication of a point by a scalar.
func (g *BJJ) ScalarMult(a curve.Point, scalar *big.Int) {
	if scalar.Sign() == 0 {
		g.SetZero()
		return
	}
	g.inner.ScalarMultiplication(a.(*BJJ).inner, scalar)
}

// ScalarBaseMult performs scalar multiplication using the base point.
func (g *BJJ) ScalarBaseMult(scalar *big.Int) {
	g.SetGenerator()
	g.ScalarMult(g, scalar)
}

// Equal checks if the given point is equal to the current point.
func (g *BJJ) Equal(a curve.Point) bool {
	return g.inner.Equal(a.(*BJJ).inner)
}

// Neg negates the given point and stores the result in g.
func (g *BJJ) Neg(a curve.Point) {
	g.inner.Neg(a.(*BJJ).inner)
}

// SetZero sets the current point to the identity element (0, 1).
func (g *BJJ) SetZero() {
	g.inner.X.SetZero() // X = 0
	g.inner.Y.SetOne()  // Y = 1
}

// Set sets g to the value of another point.
func (g *BJJ) Set(a curve.Point) {
	g.inner.Set(a.(*BJJ).inner)
}

// SetGenerator sets the point to the BabyJubJub generator.
func (g *BJJ) SetGenerator() {
	g.inner.Set(&Params.Base)
}

// String returns a string representation of the point in Twisted Edwards
// coordinates.
func (g *BJJ) String() string {
	x, y := g.Point()
	return fmt.Sprintf("%s,%s", x.String(), y.String())
}

// Marshal serializes the elliptic curve element into a byte slice.
func (p *BJJ) Marshal() []byte {
	return p.inner.Marshal()
}

// Unmarshal deserializes the elliptic curve element from a byte slice.
func (p *BJJ) Unmarshal(buf []byte) error {
	return p.inner.Unmarshal(buf)
}

// MarshalJSON serializes the elliptic curve element into a JSON byte slice.
func (p *BJJ) MarshalJSON() ([]byte, error) {
	x := types.BigInt(*p.inner.X.BigInt(new(big.Int)))
	y := types.BigInt(*p.inner.Y.BigInt(new(big.Int)))
	return json.Marshal([]types.BigInt{x, y})
}

// UnmarshalJSON deserializes the elliptic curve element from a JSON byte slice.
func (p *BJJ) UnmarshalJSON(buf []byte) error {
	if p.inner == nil {
		p.inner = new(babyjubjub.PointAffine)
	}
	var coords []types.BigInt
	if err := json.Unmarshal(buf, &coords); err != nil {
		return err
	}
	if len(coords) != 2 {
		return fmt.Errorf("expected 2 coordinates, got %d", len(coords))
	}
	p.inner.X.SetBigInt(coords[0].MathBigInt())
	p.inner.Y.SetBigInt(coords[1].MathBigInt())
	return nil
}

// MarshalCBOR serializes the elliptic curve element into a CBOR byte slice.
func (p *BJJ) MarshalCBOR() ([]byte, error) {
	x := p.inner.X.BigInt(new(big.Int))
	y := p.inner.Y.BigInt(new(big.Int))
	return cbor.Marshal([]*big.Int{x, y})
}

// UnmarshalCBOR deserializes the elliptic curve element from a CBOR byte slice.
func (p *BJJ) UnmarshalCBOR(buf []byte) error {
	if p.inner == nil {
		p.inner = new(babyjubjub.PointAffine)
	}
	var coords []*big.Int
	if err := cbor.Unmarshal(buf, &coords); err != nil {
		return err
	}
	if len(coords) != 2 {
		return fmt.Errorf("expected 2 coordinates, got %d", len(coords))
	}
	p.inner.X.SetBigInt(coords[0])
	p.inner.Y.SetBigInt(coords[1])
	return nil
}

// Point returns the X and Y coordinates of the elliptic curve element in
// Reduced Twisted Edwards coordinates.
func (p *BJJ) Point() (*big.Int, *big.Int) {
	x, y := new(big.Int), new(big.Int)
	p.inner.X.BigInt(x)
	p.inner.Y.BigInt(y)
	return x, y
}

// BigInts returns the X and Y coordinates of the elliptic curve element as
// a slice of big.Int in Reduced Twisted Edwards coordinates.
func (p *BJJ) BigInts() []*big.Int {
	x, y := p.Point()
	return []*big.Int{x, y}
}

// SetPoint sets the elliptic curve element from the X and Y coordinates in
// Reduced Twisted Edwards coordinates.
func (p *BJJ) SetPoint(x, y *big.Int) curve.Point {
	p = &BJJ{inner: new(babyjubjub.PointAffine)}
	p.inner.X.SetBigInt(x)
	p.inner.Y.SetBigInt(y)
	return p
}

func (g *BJJ) Type() string {
	return CurveType
}
