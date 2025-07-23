package blobs

import (
	"fmt"
	"math/big"
	"testing"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	qt "github.com/frankban/quicktest"
)

// TestFullEvaluation verifies the evaluation must include all 4096 elements
func TestFullEvaluation(t *testing.T) {
	c := qt.New(t)

	// Create blob with 2 elements
	blob := &kzg4844.Blob{}
	blob[31] = 1    // blob[0] = 1
	blob[32+31] = 2 // blob[1] = 2

	// Use fixed z
	z := big.NewInt(12345)

	// Get expected from KZG
	_, claim, err := ComputeProof(blob, z)
	c.Assert(err, qt.IsNil)
	expectedY := new(big.Int).SetBytes(claim[:])

	fmt.Printf("Testing evaluation with ALL 4096 elements vs only non-zero elements:\n")
	fmt.Printf("blob[0] = 1, blob[1] = 2, rest = 0\n")
	fmt.Printf("z = %s\n", z.String())
	fmt.Printf("Expected y from KZG: %s\n\n", expectedY.String())

	// Setup
	mod := bls12381.ID.ScalarField()
	rMinus1 := new(big.Int).Sub(mod, big.NewInt(1))
	generator := big.NewInt(5)
	exponent := new(big.Int).Div(rMinus1, big.NewInt(4096))
	omega := new(big.Int).Exp(generator, exponent, mod)

	// Generate all omega values in natural order
	omegasNatural := make([]*big.Int, 4096)
	omegasNatural[0] = big.NewInt(1)
	for i := 1; i < 4096; i++ {
		omegasNatural[i] = new(big.Int).Mul(omegasNatural[i-1], omega)
		omegasNatural[i].Mod(omegasNatural[i], mod)
	}

	// Create bit-reversed omega values (matching KZG's brp_roots_of_unity)
	omegas := make([]*big.Int, 4096)
	for i := 0; i < 4096; i++ {
		brpIdx := bitReverse(i, 12) // log2(4096) = 12
		omegas[i] = omegasNatural[brpIdx]
	}

	// Test 1: Evaluate with only first 2 non-zero elements
	// This actually gives the correct result since zero elements don't contribute to the sum
	fmt.Println("Test 1: Evaluating with only first 2 non-zero elements:")
	sum1 := big.NewInt(0)
	for i := 0; i < 2; i++ {
		val := big.NewInt(int64(i + 1))

		diff := new(big.Int).Sub(z, omegas[i])
		diff.Mod(diff, mod)
		invDiff := new(big.Int).ModInverse(diff, mod)

		term := new(big.Int).Mul(val, omegas[i])
		term.Mul(term, invDiff)
		term.Mod(term, mod)
		sum1.Add(sum1, term)
		sum1.Mod(sum1, mod)
	}

	// Apply factor
	zPowN := new(big.Int).Exp(z, big.NewInt(4096), mod)
	zPowNMinus1 := new(big.Int).Sub(zPowN, big.NewInt(1))
	zPowNMinus1.Mod(zPowNMinus1, mod)
	nInv := new(big.Int).ModInverse(big.NewInt(4096), mod)
	factor := new(big.Int).Mul(zPowNMinus1, nInv)
	factor.Mod(factor, mod)

	result1 := new(big.Int).Mul(factor, sum1)
	result1.Mod(result1, mod)
	fmt.Printf("Result (only 2 elements): %s\n", result1.String())
	fmt.Printf("Matches KZG: %v\n\n", result1.Cmp(expectedY) == 0)

	// However, the circuit MUST process all 4096 elements to be secure
	// Test 2: Evaluate with ALL 4096 elements (as required by the circuit)
	fmt.Println("Test 2: Evaluating with ALL 4096 elements (required for circuit security):")
	sum2 := big.NewInt(0)
	for i := 0; i < 4096; i++ {
		// Get blob value (0 for i >= 2)
		var val *big.Int
		switch i {
		case 0:
			val = big.NewInt(1)
		case 1:
			val = big.NewInt(2)
		default:
			val = big.NewInt(0)
		}

		diff := new(big.Int).Sub(z, omegas[i])
		diff.Mod(diff, mod)

		if diff.Sign() == 0 {
			// z == omega[i], skip this term
			continue
		}

		invDiff := new(big.Int).ModInverse(diff, mod)

		term := new(big.Int).Mul(val, omegas[i])
		term.Mul(term, invDiff)
		term.Mod(term, mod)
		sum2.Add(sum2, term)
		sum2.Mod(sum2, mod)
	}

	result2 := new(big.Int).Mul(factor, sum2)
	result2.Mod(result2, mod)
	fmt.Printf("Result (all 4096 elements): %s\n", result2.String())
	fmt.Printf("Matches KZG: %v\n", result2.Cmp(expectedY) == 0)

	c.Assert(result2.Cmp(expectedY), qt.Equals, 0, qt.Commentf("Full evaluation should match KZG"))
}
