package types

import (
	"fmt"
	"math/big"

	"github.com/fxamacker/cbor/v2"
)

// BigInt is a big.Int wrapper which marshals JSON to a string representation of
// the big number. Note that a nil pointer value marshals as the empty string.
type BigInt big.Int

// NewInt creates a new BigInt from the given integer value.
func NewInt(x int) *BigInt {
	return new(BigInt).SetInt(x)
}

// MarshalText returns the decimal string representation of the big number.
// If the receiver is nil, we return "0".
func (i *BigInt) MarshalText() ([]byte, error) {
	if i == nil {
		return []byte("0"), nil
	}
	return (*big.Int)(i).MarshalText()
}

// UnmarshalText parses the text representation into the big number.
func (i *BigInt) UnmarshalText(data []byte) error {
	if i == nil {
		return fmt.Errorf("cannot unmarshal into nil BigInt")
	}
	return (*big.Int)(i).UnmarshalText(data)
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// It supports both string and numeric JSON representations.
func (i *BigInt) UnmarshalJSON(data []byte) error {
	if i == nil {
		return fmt.Errorf("cannot unmarshal into nil BigInt")
	}

	// If it's a string representation (with double quotes)
	if len(data) > 0 && data[0] == '"' {
		// Remove the quotes and use UnmarshalText
		return i.UnmarshalText(data[1 : len(data)-1])
	}

	// If it's a numeric representation (without quotes)
	return i.UnmarshalText(data)
}

// MarshalCBOR explicitly encodes BigInt as a CBOR text string.
func (i *BigInt) MarshalCBOR() ([]byte, error) {
	// get the textual representation.
	txt, err := i.MarshalText()
	if err != nil {
		return nil, err
	}
	// encode that string as a CBOR text string.
	return cbor.Marshal(string(txt))
}

// UnmarshalCBOR decodes a CBOR text string into BigInt.
func (i *BigInt) UnmarshalCBOR(data []byte) error {
	var s string
	// decode the CBOR data into a string.
	if err := cbor.Unmarshal(data, &s); err != nil {
		return err
	}
	// convert the string back into BigInt.
	return i.UnmarshalText([]byte(s))
}

func (i *BigInt) GobEncode() ([]byte, error) {
	return i.MathBigInt().GobEncode()
}

func (i *BigInt) GobDecode(buf []byte) error {
	return i.MathBigInt().GobDecode(buf)
}

// String returns the string representation of the big number
func (i *BigInt) String() string {
	return (*big.Int)(i).String()
}

// SetBytes interprets buf as big-endian unsigned integer
func (i *BigInt) SetBytes(buf []byte) *BigInt {
	return (*BigInt)(i.MathBigInt().SetBytes(buf))
}

// Bytes returns the bytes representation of the big number
func (i *BigInt) Bytes() []byte {
	return (*big.Int)(i).Bytes()
}

// MathBigInt converts b to a math/big *Int.
func (i *BigInt) MathBigInt() *big.Int {
	return (*big.Int)(i)
}

// Add sum x+y
func (i *BigInt) Add(x, y *BigInt) *BigInt {
	return (*BigInt)(i.MathBigInt().Add(x.MathBigInt(), y.MathBigInt()))
}

// Sub subs x-y
func (i *BigInt) Sub(x, y *BigInt) *BigInt {
	return (*BigInt)(i.MathBigInt().Sub(x.MathBigInt(), y.MathBigInt()))
}

// Mul multiplies x*y
func (i *BigInt) Mul(x, y *BigInt) *BigInt {
	return (*BigInt)(i.MathBigInt().Mul(x.MathBigInt(), y.MathBigInt()))
}

// SetUint64 sets the value of x to the big number
func (i *BigInt) SetUint64(x uint64) *BigInt {
	return (*BigInt)(i.MathBigInt().SetUint64(x))
}

func (i *BigInt) SetInt(x int) *BigInt {
	return (*BigInt)(i.MathBigInt().SetUint64(uint64(x)))
}

// SetBigInt sets the value of x to the big number.
func (i *BigInt) SetBigInt(x *big.Int) *BigInt {
	return (*BigInt)(i.MathBigInt().Set(x))
}

// Equal helps us with go-cmp.
func (i *BigInt) Equal(j *BigInt) bool {
	if i == nil || j == nil {
		return (i == nil) == (j == nil)
	}
	return i.MathBigInt().Cmp(j.MathBigInt()) == 0
}

func (i *BigInt) ToFF(baseField *big.Int) *BigInt {
	iv := i.MathBigInt()
	z := big.NewInt(0)
	if c := iv.Cmp(baseField); c == 0 {
		return (*BigInt)(z)
	} else if c != 1 && iv.Cmp(z) != -1 {
		return (*BigInt)(iv)
	}
	return (*BigInt)(z.Mod(iv, baseField))
}
