// Package crypto provides cryptographic utilities and helper functions for
// the Vocdoni system. It includes functions for working with finite fields,
// serialization, and other cryptographic operations.
package crypto

import "math/big"

// SignatureCircuitVariableLen is the standard size in bytes for serialized
// field elements
const SignatureCircuitVariableLen = 32 // bytes

// BigIntToFFToSign transform the inputs bigInt to the field provided, if it
// is not done, the circuit will transform it during the witness calculation
// and the resulting hash will be different. Moreover, the input hash should
// be 32 bytes so if it is not, fill with zeros at the beginning of the bytes
// representation.
func BigIntToFFToSign(input, field *big.Int) []byte {
	return BigIntToBytesToSign(BigToFF(field, input))
}

// BigIntToBytesToSign converts a big.Int to a byte slice, ensuring that
// the resulting byte slice has SignatureCircuitVariableLen bytes. If the
// byte slice is shorter than SerializedFieldSize, it prepends zeros until
// the length is equal to SerializedFieldSize. If the byte slice is longer,
// it truncates it to the last SerializedFieldSize bytes.
func BigIntToBytesToSign(input *big.Int) []byte {
	return PadToSign(input.Bytes())
}

// PadToSign pads the input byte slice to ensure it has a length of
// SignatureCircuitVariableLen bytes. If the input is shorter, it prepends
// zeros until the length is equal to SignatureCircuitVariableLen. If the
// input is longer, it truncates it to the last SignatureCircuitVariableLen
// bytes.
func PadToSign(input []byte) []byte {
	// if the length of the input is less than SerializedFieldSize, pad with
	// zeros at the beginning until it reaches SerializedFieldSize
	if len(input) < SignatureCircuitVariableLen {
		for len(input) < SignatureCircuitVariableLen {
			input = append([]byte{0}, input...)
		}
	} else if len(input) > SignatureCircuitVariableLen {
		// if the length of the input is greater than SerializedFieldSize,
		// truncate it to SerializedFieldSize bytes
		input = input[len(input)-SignatureCircuitVariableLen:]
	}
	return input
}

// BigToFF function returns the finite field representation of the big.Int
// provided. It uses the curve scalar field to represent the provided number.
func BigToFF(field, iv *big.Int) *big.Int {
	z := big.NewInt(0)
	if c := iv.Cmp(field); c == 0 {
		return z
	} else if c != 1 && iv.Cmp(z) != -1 {
		return iv
	}
	return z.Mod(iv, field)
}
