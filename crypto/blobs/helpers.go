package blobs

import (
	"encoding/hex"

	"github.com/consensys/gnark/frontend"
)

// hexEncode encodes bytes to hex string with 0x prefix
func hexEncode(b []byte) string {
	return "0x" + hex.EncodeToString(b)
}

// splitIntoLimbs splits bytes into 16-byte limbs for circuit input
func splitIntoLimbs(data []byte, numLimbs int) []frontend.Variable {
	limbs := make([]frontend.Variable, numLimbs)
	limbSize := len(data) / numLimbs

	// Reverse order to match EVM format (big-endian)
	for i := range numLimbs {
		start := (numLimbs - 1 - i) * limbSize
		end := start + limbSize
		limbs[i] = hexEncode(data[start:end])
	}
	return limbs
}
