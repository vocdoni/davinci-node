package types

import (
	"encoding/hex"
	"fmt"
)

// HexBytes is a []byte which encodes as hexadecimal in json, as opposed to the
// base64 default.
type HexBytes []byte

// Hex32Bytes returns a new HexBytes padded with leading zeros to 32 bytes.
func (b HexBytes) Hex32Bytes() HexBytes {
	return b.LeftPad(32)
}

// Bytes returns the underlying byte slice of the HexBytes.
func (b *HexBytes) Bytes() []byte {
	return *b
}

// Hex returns the hexadecimal string representation of the HexBytes.
func (b *HexBytes) Hex() string {
	return hex.EncodeToString(*b)
}

// String returns the hexadecimal string representation of the HexBytes,
// prefixed with "0x".
func (b *HexBytes) String() string {
	return "0x" + b.Hex()
}

// BigInt converts the HexBytes to a BigInt.
func (b *HexBytes) BigInt() *BigInt {
	return new(BigInt).SetBytes(*b)
}

// LeftPad returns a new HexBytes padded with leading zeros to the specified
// length n. If the length of b is already n or greater, it returns a copy of b.
// Adding leading zeros does not change the value represented by the HexBytes.
func (b HexBytes) LeftPad(n int) HexBytes {
	if len(b) >= n {
		out := make(HexBytes, len(b))
		copy(out, b)
		return out
	}
	out := make(HexBytes, n)
	copy(out[n-len(b):], b)
	return out
}

// LeftTrim returns a new HexBytes with leading zeros removed. If there are no
// leading zeros, it returns a copy of b.
func (b HexBytes) LeftTrim() HexBytes {
	i := 0
	for i < len(b) && b[i] == 0 {
		i++
	}
	out := make(HexBytes, len(b)-i)
	copy(out, b[i:])
	return out
}

// RightTrim returns a new HexBytes with trailing zeros removed. If there are no
// trailing zeros, it returns a copy of b.
func (b HexBytes) RightTrim() HexBytes {
	i := len(b) - 1
	for i >= 0 && b[i] == 0 {
		i--
	}
	out := make(HexBytes, i+1)
	copy(out, b[:i+1])
	return out
}

// Equal method compares the current HexBytes with the provided one. First
// checks if both have the same length, and compare them byte per byte.
func (b HexBytes) Equal(other HexBytes) bool {
	if len(b) != len(other) {
		return false
	}
	for i := range b {
		if b[i] != other[i] {
			return false
		}
	}
	return true
}

// MarshalJSON implements the json.Marshaler interface for HexBytes. It encodes
// the byte slice as a hexadecimal string prefixed with "0x".
func (b HexBytes) MarshalJSON() ([]byte, error) {
	enc := make([]byte, hex.EncodedLen(len(b))+4)
	enc[0] = '"'
	enc[1] = '0'
	enc[2] = 'x'
	hex.Encode(enc[3:], b)
	enc[len(enc)-1] = '"'
	return enc, nil
}

// UnmarshalJSON implements the json.Unmarshaler interface for HexBytes. It
// expects a JSON string containing a hexadecimal representation, optionally
// prefixed with "0x".
func (b *HexBytes) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("invalid JSON string: %q", data)
	}
	data = data[1 : len(data)-1]

	// Strip a leading "0x" prefix, for backwards compatibility.
	if len(data) >= 2 && data[0] == '0' && (data[1] == 'x' || data[1] == 'X') {
		data = data[2:]
	}

	decLen := hex.DecodedLen(len(data))
	if cap(*b) < decLen {
		*b = make([]byte, decLen)
	} else {
		*b = (*b)[:decLen]
	}
	if _, err := hex.Decode(*b, data); err != nil {
		return err
	}
	return nil
}

// HexStringToHexBytesMustUnmarshal converts a hex string to a HexBytes.
// It strips a leading '0x' or '0X' if found, for backwards compatibility.
// Panics if the string is not a valid hex string.
func HexStringToHexBytesMustUnmarshal(hexString string) HexBytes {
	// Strip a leading "0x" prefix, for backwards compatibility.
	if len(hexString) >= 2 && hexString[0] == '0' && (hexString[1] == 'x' || hexString[1] == 'X') {
		hexString = hexString[2:]
	}
	b, err := hex.DecodeString(hexString)
	if err != nil {
		panic(err)
	}
	return b
}

// HexStringToHexBytes converts a hex string to a HexBytes.
func HexStringToHexBytes(hexString string) (HexBytes, error) {
	// Strip a leading "0x" prefix, for backwards compatibility.
	if len(hexString) >= 2 && hexString[0] == '0' && (hexString[1] == 'x' || hexString[1] == 'X') {
		hexString = hexString[2:]
	}
	b, err := hex.DecodeString(hexString)
	if err != nil {
		return nil, fmt.Errorf("invalid hex string %q: %w", hexString, err)
	}
	return b, nil
}
