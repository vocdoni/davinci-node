package blobs

import (
	"fmt"
	"math/big"
	"testing"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	qt "github.com/frankban/quicktest"
)

// TestBitReversalEvaluation tests if KZG expects bit-reversed ordering
func TestBitReversalEvaluation(t *testing.T) {
	c := qt.New(t)

	// Create blob with 2 elements
	blob := &kzg4844.Blob{}
	blob[31] = 1    // blob[0] = 1
	blob[32+31] = 2 // blob[1] = 2

	// Use fixed z
	z := big.NewInt(12345)

	// Get expected from KZG
	_, claim, err := kzg4844.ComputeProof(blob, BigIntToKZGPoint(z))
	c.Assert(err, qt.IsNil)
	expectedY := new(big.Int).SetBytes(claim[:])

	fmt.Printf("Testing bit-reversal hypothesis:\n")
	fmt.Printf("blob[0] = 1, blob[1] = 2, rest = 0\n")
	fmt.Printf("z = %s\n", z.String())
	fmt.Printf("Expected y from KZG: %s\n\n", expectedY.String())

	// Setup
	mod := bls12381.ID.ScalarField()
	rMinus1 := new(big.Int).Sub(mod, big.NewInt(1))
	generator := big.NewInt(5)
	exponent := new(big.Int).Div(rMinus1, big.NewInt(4096))
	omega := new(big.Int).Exp(generator, exponent, mod)

	// Generate omega values in natural order
	omegas := make([]*big.Int, 4096)
	omegas[0] = big.NewInt(1)
	for i := 1; i < 4096; i++ {
		omegas[i] = new(big.Int).Mul(omegas[i-1], omega)
		omegas[i].Mod(omegas[i], mod)
	}

	// Create bit-reversed omega values
	omegasBRP := make([]*big.Int, 4096)
	for i := 0; i < 4096; i++ {
		brpIdx := bitReverse(i, 12) // log2(4096) = 12
		omegasBRP[i] = omegas[brpIdx]
	}

	// Extract blob values
	blobVals := make([]*big.Int, 4096)
	for i := 0; i < 4096; i++ {
		blobVals[i] = new(big.Int).SetBytes(blob[i*32 : (i+1)*32])
	}

	// Test 1: Natural order (should NOT match KZG)
	fmt.Println("Test 1: Natural order omega values:")
	sum1 := computeSum(z, omegas, blobVals, mod)
	result1 := applyFactor(z, sum1, mod)
	fmt.Printf("Result: %s\n", result1.String())
	fmt.Printf("Matches KZG: %v\n\n", result1.Cmp(expectedY) == 0)
	c.Assert(result1.Cmp(expectedY), qt.Not(qt.Equals), 0, qt.Commentf("Natural order should NOT match KZG"))

	// Test 2: Bit-reversed omega values (should match KZG)
	fmt.Println("Test 2: Bit-reversed omega values:")
	sum2 := computeSum(z, omegasBRP, blobVals, mod)
	result2 := applyFactor(z, sum2, mod)
	fmt.Printf("Result: %s\n", result2.String())
	fmt.Printf("Matches KZG: %v\n\n", result2.Cmp(expectedY) == 0)
	c.Assert(result2.Cmp(expectedY), qt.Equals, 0, qt.Commentf("Bit-reversed omega should match KZG. Expected %s, got %s", expectedY.String(), result2.String()))

	// Test 3: Bit-reversed blob values with natural omega (should match KZG)
	fmt.Println("Test 3: Bit-reversed blob values with natural omega:")
	blobValsBRP := make([]*big.Int, 4096)
	for i := 0; i < 4096; i++ {
		brpIdx := bitReverse(i, 12)
		blobValsBRP[i] = blobVals[brpIdx]
	}
	sum3 := computeSum(z, omegas, blobValsBRP, mod)
	result3 := applyFactor(z, sum3, mod)
	fmt.Printf("Result: %s\n", result3.String())
	fmt.Printf("Matches KZG: %v\n", result3.Cmp(expectedY) == 0)
	c.Assert(result3.Cmp(expectedY), qt.Equals, 0, qt.Commentf("Bit-reversed blob should match KZG. Expected %s, got %s", expectedY.String(), result3.String()))

	// Print bit-reversal mapping for first few indices
	fmt.Printf("\nBit-reversal mapping (first 8 indices):\n")
	for i := 0; i < 8; i++ {
		fmt.Printf("i=%d -> brp(i)=%d\n", i, bitReverse(i, 12))
	}
}

func computeSum(z *big.Int, omegas []*big.Int, blobVals []*big.Int, mod *big.Int) *big.Int {
	sum := big.NewInt(0)
	for i := 0; i < 4096; i++ {
		if blobVals[i].Sign() == 0 {
			continue // Skip zero values
		}

		diff := new(big.Int).Sub(z, omegas[i])
		diff.Mod(diff, mod)

		if diff.Sign() == 0 {
			continue // Skip if z == omega[i]
		}

		invDiff := new(big.Int).ModInverse(diff, mod)
		term := new(big.Int).Mul(blobVals[i], omegas[i])
		term.Mul(term, invDiff)
		term.Mod(term, mod)
		sum.Add(sum, term)
		sum.Mod(sum, mod)
	}
	return sum
}

func applyFactor(z *big.Int, sum *big.Int, mod *big.Int) *big.Int {
	zPowN := new(big.Int).Exp(z, big.NewInt(4096), mod)
	zPowNMinus1 := new(big.Int).Sub(zPowN, big.NewInt(1))
	zPowNMinus1.Mod(zPowNMinus1, mod)
	nInv := new(big.Int).ModInverse(big.NewInt(4096), mod)
	factor := new(big.Int).Mul(zPowNMinus1, nInv)
	factor.Mod(factor, mod)
	result := new(big.Int).Mul(factor, sum)
	result.Mod(result, mod)
	return result
}
