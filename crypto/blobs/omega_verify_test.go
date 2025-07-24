package blobs

import (
	"fmt"
	"math/big"
	"testing"

	fr "github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	"github.com/consensys/gnark/frontend"
	qt "github.com/frankban/quicktest"
)

// toBig converts a frontend.Variable (expected to be *big.Int or hex/dec string in generated tables)
// into *big.Int. Adapt if you store limbs differently.
func toBig(v frontend.Variable) *big.Int {
	switch t := v.(type) {
	case *big.Int:
		return new(big.Int).Set(t)
	case string:
		b := new(big.Int)
		if len(t) > 2 && (t[:2] == "0x" || t[:2] == "0X") {
			b.SetString(t[2:], 16)
		} else {
			b.SetString(t, 10)
		}
		return b
	default:
		panic("unexpected limb type")
	}
}

// reconstructBigIntFromLimbs recomposes a little‑endian limb slice into one big.Int.
// For BLS12-381 field elements, limbs are 64-bit values.
func reconstructBigIntFromLimbs(limbs []frontend.Variable) *big.Int {
	if len(limbs) == 0 {
		return new(big.Int)
	}

	result := new(big.Int)
	limbBase := new(big.Int).Lsh(big.NewInt(1), 64) // 2^64 for 64-bit limbs

	// Reconstruct in little-endian order (limbs[0] is least significant)
	for i := len(limbs) - 1; i >= 0; i-- {
		limbVal := toBig(limbs[i])
		result.Mul(result, limbBase)
		result.Add(result, limbVal)
	}

	return result
}

// TestOmegaVerification verifies that we can reconstruct the same omega values as the Go implementation
// and that they match the pre-computed values in omega_table.go
func TestOmegaVerification(t *testing.T) {
	c := qt.New(t)

	// Generate omega values using Go implementation approach
	mod := fr.Modulus()
	n := 4096
	toBig := func(e fr.Element) *big.Int { return e.BigInt(new(big.Int)) }

	// Generate evaluation domain using go-eth-kzg's approach
	omegaDynamic := make([]*big.Int, n)

	// Initialize the primitive root of unity used by go-eth-kzg
	var rootOfUnity fr.Element
	_, err := rootOfUnity.SetString("10238227357739495823651030575849232062558860180284477541189508159991286009131")
	c.Assert(err, qt.IsNil)

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
	for i := 0; i < n; i++ {
		bitRevIndex := bitReverse(i, 12)
		omegaDynamic[i] = toBig(domainRoots[bitRevIndex])
	}

	// Now compute nInverse
	nBig := big.NewInt(int64(n))
	nInvDynamic := new(big.Int).ModInverse(nBig, mod)

	fmt.Printf("=== OMEGA VERIFICATION TEST ===\n")
	fmt.Printf("Testing dynamic generation vs omega_table.go values\n")
	fmt.Printf("Go omega[0] = %s\n", omegaDynamic[0].Text(16))
	fmt.Printf("Go omega[1] = %s\n", omegaDynamic[1].Text(16))
	fmt.Printf("Go omega[2] = %s\n", omegaDynamic[2].Text(16))
	fmt.Printf("Go nInverse = %s\n", nInvDynamic.Text(16))

	// Extract values from omega_table.go for comparison
	// Convert emulated field elements (reconstructed from limbs) to big.Int for comparison
	omegaTable := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		// Use the omegaAt(i) function to get emulated element from limbs
		omegaElement := omegaAt(i)
		// Convert the limbs to a big.Int
		omegaTable[i] = reconstructBigIntFromLimbs(omegaElement.Limbs)
	}
	// Convert nInverse limbs to big.Int
	nInvTable := reconstructBigIntFromLimbs(nInverse.Limbs)

	fmt.Printf("\n=== COMPARING FIRST 100 OMEGA VALUES ===\n")

	// Verify that omega[0] is 1
	c.Assert(omegaDynamic[0].Cmp(big.NewInt(1)), qt.Equals, 0, qt.Commentf("omega[0] should be 1"))
	c.Assert(omegaTable[0].Cmp(big.NewInt(1)), qt.Equals, 0, qt.Commentf("omega_table[0] should be 1"))

	// Compare the first 100 omega values
	matchingCount := 0
	mismatchCount := 0

	for i := 0; i < 100; i++ {
		if omegaDynamic[i].Cmp(omegaTable[i]) == 0 {
			matchingCount++
		} else {
			mismatchCount++
			fmt.Printf("MISMATCH at omega[%d]:\n", i)
			fmt.Printf("  Dynamic: %s\n", omegaDynamic[i].Text(16))
			fmt.Printf("  Table:   %s\n", omegaTable[i].Text(16))
		}

		// Assert each value matches
		c.Assert(omegaDynamic[i].Cmp(omegaTable[i]), qt.Equals, 0,
			qt.Commentf("omega[%d] mismatch: dynamic=%s, table=%s",
				i, omegaDynamic[i].Text(16), omegaTable[i].Text(16)))
	}

	fmt.Printf("Checked first 100 omega values: %d matches, %d mismatches\n", matchingCount, mismatchCount)

	// Verify nInverse values match
	c.Assert(nInvDynamic.Cmp(nInvTable), qt.Equals, 0,
		qt.Commentf("nInverse mismatch: dynamic=%s, table=%s",
			nInvDynamic.Text(16), nInvTable.Text(16)))

	fmt.Printf("nInverse values match: %s\n", nInvDynamic.Text(16))

	// Test a few specific indices to ensure bit-reversal is working correctly
	testIndices := []int{0, 1, 2, 3, 4, 10, 50, 99}
	fmt.Printf("\n=== SPOT CHECK SPECIFIC INDICES ===\n")
	for _, i := range testIndices {
		fmt.Printf("omega[%d]: dynamic=%s, table=%s, match=%t\n",
			i, omegaDynamic[i].Text(16), omegaTable[i].Text(16),
			omegaDynamic[i].Cmp(omegaTable[i]) == 0)

		c.Assert(omegaDynamic[i].Cmp(omegaTable[i]), qt.Equals, 0,
			qt.Commentf("omega[%d] should match", i))
	}

	fmt.Printf("\n=== ALL OMEGA VERIFICATIONS PASSED ===\n")
}
