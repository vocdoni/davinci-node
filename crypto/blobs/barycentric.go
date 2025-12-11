package blobs

import (
	"fmt"
	"math/big"

	fr "github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	gethkzg "github.com/ethereum/go-ethereum/crypto/kzg4844"
)

// EvaluateBarycentricNative evaluates a polynomial in evaluation form using the barycentric formula.
// It has been implemented in Go to match the circuit logic and to debug potential issues.
// This function computes the barycentric evaluation of a KZG blob at a given evaluation point z.
//
// The function implements the barycentric evaluation formula:
//
//	y = (z^4096 - 1) / 4096 * Σᵢ (dᵢ * ωᵢ / (z - ωᵢ))
//
// Where:
//   - dᵢ are the blob data values (polynomial evaluations at domain points)
//   - ωᵢ are the roots of unity forming the evaluation domain
//   - z is the evaluation point
func EvaluateBarycentricNative(blob *gethkzg.Blob, z *big.Int, debug bool) (*big.Int, error) {
	if len(blob) != 32*4096 {
		return nil, fmt.Errorf("blob length is %d, want 131072", len(blob))
	}

	// Initialize field arithmetic helpers
	mod := fr.Modulus()
	n := 4096
	nBig := big.NewInt(int64(n))
	nInv := new(big.Int).ModInverse(nBig, mod) // 4096⁻¹ mod p
	modBig := func(x *big.Int) *big.Int { return new(big.Int).Mod(x, mod) }
	toBig := func(e fr.Element) *big.Int { return e.BigInt(new(big.Int)) }

	if debug {
		fmt.Println("=== GO DEBUG START ===")
		fmt.Printf("Z: %s\n", z.Text(16))
		fmt.Printf("First 5 blob values: %s %s %s %s %s\n",
			blobCell(blob, 0).Text(16),
			blobCell(blob, 1).Text(16),
			blobCell(blob, 2).Text(16),
			blobCell(blob, 3).Text(16),
			blobCell(blob, 4).Text(16))
	}

	// Generate evaluation domain using go-eth-kzg's approach
	omega := make([]*big.Int, n)

	// Initialize the primitive root of unity used by go-eth-kzg
	var rootOfUnity fr.Element
	_, err := rootOfUnity.SetString("10238227357739495823651030575849232062558860180284477541189508159991286009131")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize root of unity: %v", err)
	}

	// Compute the generator for our 4096-element domain
	expo := new(big.Int).SetInt64(1 << 20) // 2^20 for 4096 domain size
	var generator fr.Element
	generator.Exp(rootOfUnity, expo)

	// Generate all domain roots in natural order
	domainRoots := make([]fr.Element, n)
	domainRoots[0].SetOne()
	for i := 1; i < n; i++ {
		domainRoots[i].Mul(&domainRoots[i-1], &generator)
	}

	// Apply bit-reversal permutation to match go-eth-kzg's expected ordering
	for i := range n {
		bitRevIndex := bitReverse(i, 12)
		omega[i] = toBig(domainRoots[bitRevIndex])
	}

	if debug {
		fmt.Println("=== BLOCK 1: DIFFERENCES AND ZERO DETECTION ===")
		fmt.Printf("First 5 omega values: %s %s %s %s %s\n",
			omega[0].Text(16), omega[1].Text(16), omega[2].Text(16), omega[3].Text(16), omega[4].Text(16))

		// Show first few differences
		fmt.Printf("First 5 differences (z - omega[i]): ")
		for i := 0; i < 5; i++ {
			diff := new(big.Int).Sub(z, omega[i])
			diff.Mod(diff, mod)
			fmt.Printf("%s ", diff.Text(16))
		}
		fmt.Println()

		// Show isZero flags
		fmt.Printf("First 5 isZero flags: ")
		for i := 0; i < 5; i++ {
			isZero := omega[i].Cmp(z) == 0
			fmt.Printf("%t ", isZero)
		}
		fmt.Println()
	}

	// Early exit optimization: if z equals any omega value, return the corresponding blob value
	for k, w := range omega {
		if w.Cmp(z) == 0 {
			if debug {
				fmt.Printf("=== EARLY EXIT: z equals omega[%d], returning blob[%d] = %s ===\n",
					k, k, modBig(blobCell(blob, k)).Text(16))
			}
			return modBig(blobCell(blob, k)), nil
		}
	}

	// Compute the barycentric sum: Σᵢ (dᵢ * ωᵢ / (z - ωᵢ))
	sum := big.NewInt(0)
	var firstTerms [5]*big.Int
	var firstInvs [5]*big.Int

	if debug {
		fmt.Println("=== BLOCK 2: INDIVIDUAL INVERSIONS ===")
	}

	for i := 0; i < n; i++ {
		// Extract blob data value at position i
		d := modBig(blobCell(blob, i))
		if d.Sign() == 0 {
			continue // Skip zero terms for efficiency
		}

		// Compute the denominator (z - ωᵢ) and its inverse
		diff := new(big.Int).Sub(z, omega[i])
		diff.Mod(diff, mod)
		inv := new(big.Int).ModInverse(diff, mod)

		// Store first few inverses for debug
		if debug && i < 5 {
			firstInvs[i] = new(big.Int).Set(inv)
		}

		// Compute term: dᵢ * ωᵢ / (z - ωᵢ)
		term := new(big.Int).Mul(d, omega[i])
		term.Mul(term, inv)
		term.Mod(term, mod)

		// Store first few terms for debug
		if debug && i < 5 {
			firstTerms[i] = new(big.Int).Set(term)
		}

		// Accumulate the sum
		sum.Add(sum, term).Mod(sum, mod)
	}

	if debug {
		fmt.Printf("First 5 inv values: %s %s %s %s %s\n",
			firstInvs[0].Text(16), firstInvs[1].Text(16), firstInvs[2].Text(16), firstInvs[3].Text(16), firstInvs[4].Text(16))
		fmt.Println("=== BLOCK 3: SUMMATION ===")
		fmt.Printf("First 5 terms: %s %s %s %s %s\n",
			firstTerms[0].Text(16), firstTerms[1].Text(16), firstTerms[2].Text(16), firstTerms[3].Text(16), firstTerms[4].Text(16))
		fmt.Printf("Sum: %s\n", sum.Text(16))
	}

	// Compute the scaling factor: (z^4096 - 1) / 4096
	zPowN := new(big.Int).Exp(z, nBig, mod)
	zPowN.Sub(zPowN, big.NewInt(1)).Mod(zPowN, mod)
	factor := new(big.Int).Mul(zPowN, nInv)
	factor.Mod(factor, mod)

	// Compute final result: y = factor * sum
	result := new(big.Int).Mul(sum, factor)
	result.Mod(result, mod)

	if debug {
		fmt.Println("=== BLOCK 4: FACTOR COMPUTATION ===")
		fmt.Printf("z^4096: %s\n", zPowN.Text(16))
		fmt.Printf("nInverse: %s\n", nInv.Text(16))
		fmt.Printf("factor: %s\n", factor.Text(16))
		fmt.Printf("yBary (factor * sum): %s\n", result.Text(16))
		fmt.Println("=== BLOCK 5: FINAL SELECTION ===")
		fmt.Printf("final: %s\n", result.Text(16))
	}

	return result, nil
}

// blobCell extracts the i-th 32-byte element from the blob and converts it to a big integer.
// The blob stores field elements in big-endian format.
func blobCell(blob *gethkzg.Blob, i int) *big.Int {
	start := i * 32
	return new(big.Int).SetBytes(blob[start : start+32])
}

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
