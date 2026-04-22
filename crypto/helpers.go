// Package crypto provides cryptographic utilities and helper functions for
// the Vocdoni system. It includes functions for working with finite fields,
// serialization, and other cryptographic operations.
package crypto

// SignatureCircuitVariableLen is the standard size in bytes for serialized
// field elements
const SignatureCircuitVariableLen = 32 // bytes

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
