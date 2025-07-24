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

// bitReverse reverses the bits of n considering log2n bits
// Bit‑reverses the low log2n bits of n.
func bitReverse(n, log2n int) int {
	rev := 0
	for i := 0; i < log2n; i++ {
		if (n>>i)&1 == 1 {
			rev |= 1 << (log2n - 1 - i)
		}
	}
	return rev
}
