package types

import (
	"encoding/hex"
	"fmt"
)

// HexBytes is a []byte which encodes as hexadecimal in json, as opposed to the
// base64 default.
type HexBytes []byte

func (b *HexBytes) Bytes() []byte {
	return *b
}

func (b *HexBytes) String() string {
	return "0x" + hex.EncodeToString(*b)
}

func (b *HexBytes) BigInt() *BigInt {
	return new(BigInt).SetBytes(*b)
}

func (b HexBytes) MarshalJSON() ([]byte, error) {
	enc := make([]byte, hex.EncodedLen(len(b))+4)
	enc[0] = '"'
	enc[1] = '0'
	enc[2] = 'x'
	hex.Encode(enc[3:], b)
	enc[len(enc)-1] = '"'
	return enc, nil
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
