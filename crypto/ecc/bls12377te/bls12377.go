package bls12377te

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sync"

	"github.com/consensys/gnark-crypto/ecc/bls12-377/twistededwards"
	"github.com/fxamacker/cbor/v2"
	curve "github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/types"
)

// CurveType identifies the BLS12-377 twisted Edwards curve implementation.
const CurveType = "bls12377_te"

var params twistededwards.CurveParams

func init() {
	params = twistededwards.GetEdwardsCurve()
}

// Point represents a BLS12-377 twisted Edwards point in affine coordinates.
type Point struct {
	inner *twistededwards.PointAffine
	lock  sync.Mutex
}

// New returns a new point initialised to the identity element.
func New() curve.Point {
	p := &Point{inner: new(twistededwards.PointAffine)}
	p.SetZero()
	return p
}

// New returns a new point initialised to the identity element.
func (p *Point) New() curve.Point {
	return New()
}

// Order returns the subgroup order.
func (p *Point) Order() *big.Int {
	return new(big.Int).Set(&params.Order)
}

// Add sets p = a + b.
func (p *Point) Add(a, b curve.Point) {
	p.inner.Add(a.(*Point).inner, b.(*Point).inner)
}

// SafeAdd sets p = a + b using an internal lock.
func (p *Point) SafeAdd(a, b curve.Point) {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.Add(a, b)
}

// ScalarMult sets p = scalar * a.
func (p *Point) ScalarMult(a curve.Point, scalar *big.Int) {
	if scalar == nil || scalar.Sign() == 0 {
		p.SetZero()
		return
	}
	p.inner.ScalarMultiplication(a.(*Point).inner, scalar)
}

// ScalarBaseMult sets p = scalar * G.
func (p *Point) ScalarBaseMult(scalar *big.Int) {
	p.SetGenerator()
	p.ScalarMult(p, scalar)
}

// Marshal serialises the point.
func (p *Point) Marshal() []byte {
	return p.inner.Marshal()
}

// Unmarshal deserialises the point.
func (p *Point) Unmarshal(buf []byte) error {
	return p.inner.Unmarshal(buf)
}

// MarshalJSON serialises the point to JSON.
func (p *Point) MarshalJSON() ([]byte, error) {
	x := types.BigInt(*p.inner.X.BigInt(new(big.Int)))
	y := types.BigInt(*p.inner.Y.BigInt(new(big.Int)))
	return json.Marshal([]types.BigInt{x, y})
}

// UnmarshalJSON deserialises the point from JSON.
func (p *Point) UnmarshalJSON(buf []byte) error {
	if p.inner == nil {
		p.inner = new(twistededwards.PointAffine)
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

// MarshalCBOR serialises the point using CBOR.
func (p *Point) MarshalCBOR() ([]byte, error) {
	x := p.inner.X.BigInt(new(big.Int))
	y := p.inner.Y.BigInt(new(big.Int))
	return cbor.Marshal([]*big.Int{x, y})
}

// UnmarshalCBOR deserialises the point from CBOR.
func (p *Point) UnmarshalCBOR(buf []byte) error {
	if p.inner == nil {
		p.inner = new(twistededwards.PointAffine)
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

// Equal returns true if a == p.
func (p *Point) Equal(a curve.Point) bool {
	return p.inner.Equal(a.(*Point).inner)
}

// Neg sets p = -a.
func (p *Point) Neg(a curve.Point) {
	p.inner.Neg(a.(*Point).inner)
}

// SetZero sets p to the identity element.
func (p *Point) SetZero() {
	p.inner.X.SetZero()
	p.inner.Y.SetOne()
}

// Set copies a into p.
func (p *Point) Set(a curve.Point) {
	p.inner.Set(a.(*Point).inner)
}

// SetGenerator sets p to the curve generator.
func (p *Point) SetGenerator() {
	p.inner.Set(&params.Base)
}

// String returns a comma separated affine representation.
func (p *Point) String() string {
	x, y := p.Point()
	return fmt.Sprintf("%s,%s", x.String(), y.String())
}

// Point returns affine coordinates as big.Int.
func (p *Point) Point() (*big.Int, *big.Int) {
	x, y := new(big.Int), new(big.Int)
	p.inner.X.BigInt(x)
	p.inner.Y.BigInt(y)
	return x, y
}

// BigInts returns affine coordinates as []*big.Int.
func (p *Point) BigInts() []*big.Int {
	x, y := p.Point()
	return []*big.Int{x, y}
}

// SetPoint sets p from affine coordinates and returns it.
func (p *Point) SetPoint(x, y *big.Int) curve.Point {
	p.inner = new(twistededwards.PointAffine)
	p.inner.X.SetBigInt(x)
	p.inner.Y.SetBigInt(y)
	return p
}

// Type returns the curve identifier.
func (p *Point) Type() string {
	return CurveType
}
