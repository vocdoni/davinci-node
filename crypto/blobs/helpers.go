package blobs

// bitReverse reverses the bits of n considering log2n bits
// Bit‑reverses the low log2n bits of n.
func bitReverse(n, log2n int) int {
	rev := 0
	for i := range log2n {
		if (n>>i)&1 == 1 {
			rev |= 1 << (log2n - 1 - i)
		}
	}
	return rev
}
