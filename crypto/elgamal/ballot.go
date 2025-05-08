package elgamal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/curves"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/format"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

type Ballot struct {
	CurveType   string                             `json:"curveType,omitempty"`
	Ciphertexts [types.FieldsPerBallot]*Ciphertext `json:"ciphertexts"`
}

// NewBallot creates a new Ballot for the given curve.
func NewBallot(curve ecc.Point) *Ballot {
	z := &Ballot{
		CurveType:   curve.Type(),
		Ciphertexts: [types.FieldsPerBallot]*Ciphertext{},
	}
	for i := range z.Ciphertexts {
		z.Ciphertexts[i] = NewCiphertext(curve)
	}
	return z
}

// Valid method checks if the Ballot is valid. A ballot is valid if all its
// Ciphertexts are valid (not nil) and the CurveType is supported.
func (z *Ballot) Valid() bool {
	for _, c := range z.Ciphertexts {
		if c == nil {
			return false
		}
	}
	return curves.IsValid(z.CurveType)
}

// Encrypt encrypts a message using the public key provided as elliptic curve
// point. The randomness k can be provided or nil to generate a new one. Each
// ciphertext uses a different k derived from the previous one using mimc7 hash
// function. The first k is the hash of the provided one.
func (z *Ballot) Encrypt(message [types.FieldsPerBallot]*big.Int, publicKey ecc.Point, k *big.Int) (*Ballot, error) {
	var err error
	if k == nil {
		k, err = RandK()
		if err != nil {
			return nil, fmt.Errorf("elgamal encryption failed: %w", err)
		}
	}
	lastK, err := mimc7.Hash([]*big.Int{k}, nil)
	if err != nil {
		return nil, err
	}
	for i := range z.Ciphertexts {
		if _, err := z.Ciphertexts[i].Encrypt(message[i], publicKey, lastK); err != nil {
			return nil, err
		}
		lastK, err = mimc7.Hash([]*big.Int{lastK}, nil)
		if err != nil {
			return nil, err
		}
	}
	return z, nil
}

// Add adds two Ballots and stores the result in the receiver, which is also returned.
func (z *Ballot) Add(x, y *Ballot) *Ballot {
	for i := range z.Ciphertexts {
		z.Ciphertexts[i].Add(x.Ciphertexts[i], y.Ciphertexts[i])
	}
	return z
}

// BigInts returns a slice with 8*4 BigInts, namely the coords of each Ciphertext
// C1.X, C1.Y, C2.X, C2.Y as little-endian, in reduced twisted edwards form.
func (z *Ballot) BigInts() []*big.Int {
	list := []*big.Int{}
	for _, z := range z.Ciphertexts {
		c1x, c1y := z.C1.Point()
		c2x, c2y := z.C2.Point()
		list = append(list, c1x, c1y, c2x, c2y)
	}
	return list
}

func (z *Ballot) SetBigInts(list []*big.Int) (*Ballot, error) {
	// check if the curve type is valid
	if !curves.IsValid(z.CurveType) {
		return nil, fmt.Errorf("invalid curve type: %s", z.CurveType)
	}
	// check if the list has the right length
	if len(list) != 8*4 {
		return nil, fmt.Errorf("expected 8*4 BigInts, got %d", len(list))
	}
	// compose the ciphertexts
	z.Ciphertexts = [types.FieldsPerBallot]*Ciphertext{}
	for i := range z.Ciphertexts {
		c1x, c1y := list[i*4], list[i*4+1]
		c2x, c2y := list[i*4+2], list[i*4+3]
		z.Ciphertexts[i] = &Ciphertext{
			C1: curves.New(z.CurveType).SetPoint(c1x, c1y),
			C2: curves.New(z.CurveType).SetPoint(c2x, c2y),
		}
	}
	return z, nil
}

// Serialize returns a slice of len N*4*32 bytes,
// representing each Ciphertext C1.X, C1.Y, C2.X, C2.Y as little-endian,
// in reduced twisted edwards form.
func (z *Ballot) Serialize() []byte {
	var buf bytes.Buffer
	for _, z := range z.Ciphertexts {
		buf.Write(z.Serialize())
	}
	return buf.Bytes()
}

// Deserialize reconstructs a Ballot from a slice of bytes.
// The input must be of len N*4*32 bytes (otherwise it returns an error),
// representing each Ciphertext C1.X, C1.Y, C2.X, C2.Y as little-endian,
// in reduced twisted edwards form.
func (z *Ballot) Deserialize(data []byte) error {
	// Validate the input length
	if len(data) != SerializedBallotSize {
		return fmt.Errorf("invalid input length for Ballot: got %d bytes, expected %d bytes", len(data), SerializedBallotSize)
	}
	for i := range z.Ciphertexts {
		err := z.Ciphertexts[i].Deserialize(data[i*sizeCiphertext : (i+1)*sizeCiphertext])
		if err != nil {
			return err
		}
	}
	return nil
}

// String returns a string representation of the Ballot.
func (z *Ballot) String() string {
	b, err := json.Marshal(z)
	if b == nil || err != nil {
		return ""
	}
	return string(b)
}

// ToGnark returns z as the struct used by gnark,
// with the points in reduced twisted edwards format
func (z *Ballot) ToGnark() *circuits.Ballot {
	gz := &circuits.Ballot{}
	for i := range z.Ciphertexts {
		gz[i] = *z.Ciphertexts[i].ToGnark()
	}
	return gz
}

// ToGnarkEmulatedBN254 returns z as the struct used by gnark,
// with the points in reduced twisted edwards format
// but as emulated.Element[sw_bn254.ScalarField] instead of frontend.Variable
func (z *Ballot) ToGnarkEmulatedBN254() *circuits.EmulatedBallot[sw_bn254.ScalarField] {
	eb := &circuits.EmulatedBallot[sw_bn254.ScalarField]{}
	for i, z := range z.Ciphertexts {
		c1x, c1y := z.C1.Point()
		c2x, c2y := z.C2.Point()
		eb[i] = circuits.EmulatedCiphertext[sw_bn254.ScalarField]{
			C1: circuits.EmulatedPoint[sw_bn254.ScalarField]{
				X: emulated.ValueOf[sw_bn254.ScalarField](c1x),
				Y: emulated.ValueOf[sw_bn254.ScalarField](c1y),
			},
			C2: circuits.EmulatedPoint[sw_bn254.ScalarField]{
				X: emulated.ValueOf[sw_bn254.ScalarField](c2x),
				Y: emulated.ValueOf[sw_bn254.ScalarField](c2y),
			},
		}
	}
	return eb
}

// FromRTEtoTE converts the Ballot from reduced twisted Edwards to twisted
// Edwards format. It returns a new Ballot with the same Ciphertexts but in
// twisted Edwards format.
func (z *Ballot) FromRTEtoTE() *Ballot {
	teBallot := NewBallot(curves.New(z.CurveType))
	for i := range z.Ciphertexts {
		teBallot.Ciphertexts[i].C1 = teBallot.Ciphertexts[i].C1.SetPoint(
			format.FromRTEtoTE(z.Ciphertexts[i].C1.Point()))
		teBallot.Ciphertexts[i].C2 = teBallot.Ciphertexts[i].C2.SetPoint(
			format.FromRTEtoTE(z.Ciphertexts[i].C2.Point()))
	}
	return teBallot
}

func (z *Ballot) FromTEtoRTE() *Ballot {
	teBallot := NewBallot(curves.New(z.CurveType))
	for i := range z.Ciphertexts {
		teBallot.Ciphertexts[i].C1 = teBallot.Ciphertexts[i].C1.SetPoint(
			format.FromTEtoRTE(z.Ciphertexts[i].C1.Point()))
		teBallot.Ciphertexts[i].C2 = teBallot.Ciphertexts[i].C2.SetPoint(
			format.FromTEtoRTE(z.Ciphertexts[i].C2.Point()))
	}
	return teBallot
}
