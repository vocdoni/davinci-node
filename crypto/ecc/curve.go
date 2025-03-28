// Package ecc provides an interface and implementations for elliptic curve cryptography operations.
// It defines a common interface for different elliptic curve implementations and provides
// concrete implementations for various curves used in the Vocdoni system.
package ecc

import (
	"math/big"
)

// Point defines the common operations that can be performed on elliptic curve group elements.
// It represents the affine coordinates of a point on an elliptic curve and provides methods
// for arithmetic operations, serialization, and comparison.
type Point interface {
	// New returns a new elliptic curve point.
	New() Point

	// Order returns the order of the elliptic curve group.
	// This is the number of elements in the group, represented as a big integer.
	Order() *big.Int

	// Add adds two elliptic curve group elements and stores the result in the receiver.
	// a and b are the elements to be added.
	Add(a, b Point)

	// SafeAdd adds two elliptic curve group elements and stores the result in the receiver.
	// It is thread-safe, ensuring exclusive access to the receiver during the operation.
	SafeAdd(a, b Point)

	// ScalarMult performs scalar multiplication of an elliptic curve element.
	// Multiplies the group element a by the scalar value.
	ScalarMult(a Point, scalar *big.Int)

	// ScalarBaseMult performs scalar multiplication of the generator point by a scalar value.
	// The receiver is set to the result of multiplying the generator point by the scalar.
	ScalarBaseMult(scalar *big.Int)

	// Marshal serializes the elliptic curve element into a byte slice.
	// The output byte slice can be used to store or transmit the element.
	Marshal() []byte

	// Unmarshal deserializes a byte slice into an elliptic curve element.
	// The input buf must represent a valid serialized point, or an error will be returned.
	Unmarshal(buf []byte) error

	// MarshalJSON serializes the elliptic curve element into a JSON byte slice.
	MarshalJSON() ([]byte, error)

	// UnmarshalJSON deserializes a JSON byte slice into an elliptic curve element.
	UnmarshalJSON(buf []byte) error

	// MarshalCBOR serializes the elliptic curve element into a CBOR byte slice.
	MarshalCBOR() ([]byte, error)

	// UnmarshalCBOR deserializes a CBOR byte slice into an elliptic curve element.
	UnmarshalCBOR(buf []byte) error

	// Equal checks if two elliptic curve elements are equal.
	// Returns true if the elements are identical, false otherwise.
	Equal(a Point) bool

	// Neg negates an elliptic curve element, effectively computing its inverse.
	Neg(a Point)

	// SetZero sets the elliptic curve element to the zero value (point at infinity).
	// This point acts as the identity element in elliptic curve arithmetic.
	SetZero()

	// Set sets the value of the receiver to be equal to another elliptic curve element.
	Set(a Point)

	// SetGenerator sets the elliptic curve element to the generator point.
	// Returns the receiver after setting the generator point.
	SetGenerator()

	// String returns the hexadecimal string representation of the elliptic curve element.
	// Useful for debugging or displaying the group element in a human-readable form.
	String() string

	// Point returns the X and Y coordinates of the elliptic curve element.
	Point() (*big.Int, *big.Int)

	// SetPoint sets the X and Y coordinates of the elliptic curve element. Modifies the receiver.
	SetPoint(x, y *big.Int) Point

	// Type returns the type of the elliptic curve element.
	Type() string
}
