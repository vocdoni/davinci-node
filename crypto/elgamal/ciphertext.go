package elgamal

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/vocdoni/arbo"
	"github.com/vocdoni/davinci-node/crypto"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/spec/params"
	gelgamal "github.com/vocdoni/gnark-crypto-primitives/elgamal"
)

// sizes in bytes needed to serialize a Ballot
const (
	sizeCoord            = crypto.SignatureCircuitVariableLen
	sizePoint            = 2 * sizeCoord
	sizeCiphertext       = 2 * sizePoint
	SerializedBallotSize = params.FieldsPerBallot * sizeCiphertext
)

// BigIntsPerCiphertext is 4 since each Ciphertext has C1.X, C1.Y, C2.X and
// C2.Y coords
const BigIntsPerCiphertext = 4

// Ciphertext represents an ElGamal encrypted message with homomorphic properties.
// It is a wrapper for convenience of the elGamal ciphersystem that encapsulates the two points of a ciphertext.
type Ciphertext struct {
	C1 ecc.Point `json:"c1"`
	C2 ecc.Point `json:"c2"`
}

// NewCiphertext creates a new Ciphertext on the same curve as the given Point.
// The Point must be one on of the supported curves by crypto/ecc/curves package,
// can be easily created with curves.New(type)
func NewCiphertext(curve ecc.Point) *Ciphertext {
	return &Ciphertext{C1: curve.New(), C2: curve.New()}
}

// IsZero checks if the Ciphertext is zero, meaning both C1 and C2 are the zero
// point of the curve.
func (z *Ciphertext) IsZero(curve ecc.Point) bool {
	zero := curve.New()
	return z.C1.Equal(zero) && z.C2.Equal(zero)
}

// Encrypt encrypts a message using the public key provided as elliptic curve
// point. If k is nil, returns an error.
func (z *Ciphertext) Encrypt(message *big.Int, publicKey ecc.Point, k *big.Int) (*Ciphertext, error) {
	if k == nil {
		return nil, fmt.Errorf("k cannot be nil")
	}
	z.C1, z.C2 = EncryptWithK(publicKey, message, k)
	return z, nil
}

// Add adds two Ciphertext and stores the result in z, which is also returned.
func (z *Ciphertext) Add(x, y *Ciphertext) *Ciphertext {
	z.C1.SafeAdd(x.C1, y.C1)
	z.C2.SafeAdd(x.C2, y.C2)
	return z
}

// Serialize returns a slice of len 4*32 bytes,
// representing the C1.X, C1.Y, C2.X, C2.Y as little-endian,
// in reduced twisted edwards form.
func (z *Ciphertext) Serialize() []byte {
	var buf bytes.Buffer
	c1x, c1y := z.C1.Point()
	c2x, c2y := z.C2.Point()
	for _, bi := range []*big.Int{c1x, c1y, c2x, c2y} {
		buf.Write(arbo.BigIntToBytes(sizeCoord, bi))
	}
	return buf.Bytes()
}

// Deserialize reconstructs an Ciphertext from a slice of bytes.
// The input must be of len 4*32 bytes (otherwise it returns an error),
// representing the C1.X, C1.Y, C2.X, C2.Y as little-endian,
// in reduced twisted edwards form.
func (z *Ciphertext) Deserialize(data []byte) error {
	// Validate the input length
	if len(data) != sizeCiphertext {
		return fmt.Errorf("invalid input length for Ciphertext: got %d bytes, expected %d bytes", len(data), sizeCiphertext)
	}

	// Helper function to extract *big.Int from a serialized slice
	readBigInt := func(offset int) *big.Int {
		return arbo.BytesToBigInt(data[offset : offset+sizeCoord])
	}
	// Deserialize each field
	z.C1 = z.C1.SetPoint(
		readBigInt(0*sizeCoord),
		readBigInt(1*sizeCoord),
	)
	z.C2 = z.C2.SetPoint(
		readBigInt(2*sizeCoord),
		readBigInt(3*sizeCoord),
	)
	return nil
}

// String returns a string representation of the Ciphertext.
func (z *Ciphertext) String() string {
	if z == nil || z.C1 == nil || z.C2 == nil {
		return "{C1: nil, C2: nil}"
	}
	return fmt.Sprintf("{C1: %s, C2: %s}", z.C1.String(), z.C2.String())
}

// ToGnark returns z as the struct used by gnark,
// with the points in reduced twisted edwards format
func (z *Ciphertext) ToGnark() *gelgamal.Ciphertext {
	// TODO: panic if z.C1 or z.C2 is not TE
	c1x, c1y := z.C1.Point()
	c2x, c2y := z.C2.Point()
	return &gelgamal.Ciphertext{
		C1: twistededwards.Point{X: c1x, Y: c1y},
		C2: twistededwards.Point{X: c2x, Y: c2y},
	}
}
