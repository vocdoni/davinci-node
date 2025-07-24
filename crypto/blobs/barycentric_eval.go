// File: barycentric_eval.go
package blobs

import (
	"fmt"
	"math/big"

	fr "github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
)

// EvaluateBlobBarycentric computes, for a 4 096‑element blob, the value
//
//	y = (z⁴⁰⁹⁶ – 1) / 4096 · Σᵢ  dᵢ · ωᵢ / (z – ωᵢ)
//
// exactly as in `c‑kzg‑4844/eip4844.c:evaluate_polynomial_in_evaluation_form`.
//
//   - `blob` must be exactly 4096·32 bytes (type supplied by `go‑ethereum`).
//   - `z` must already be reduced modulo the BLS12‑381 scalar‑field modulus
//     (the 250‑bit masking done by your ComputeEvaluationPoint is fine).
//   - When `debug` is true the function prints an execution trace.
func EvaluateBlobBarycentric(blob *kzg4844.Blob, z *big.Int, debug bool) (*big.Int, error) {
	const n = 4096
	if len(blob) != 32*n {
		return nil, fmt.Errorf("blob length is %d, want 131072", len(blob))
	}

	mod := fr.Modulus()
	modBig := func(x *big.Int) *big.Int { return new(big.Int).Mod(x, mod) }

	//------------------------------------------------------------------//
	// 1. Build ω (BRP order) - matching scripts/gen_omega_table.go exactly
	//------------------------------------------------------------------//

	// Generate primitive 4096-th root of unity using generator 5
	pMinus1 := new(big.Int).Sub(mod, big.NewInt(1))
	generator := big.NewInt(5)
	exponent := new(big.Int).Div(pMinus1, big.NewInt(n))
	root4096 := new(big.Int).Exp(generator, exponent, mod)

	// Generate all 4096 roots in natural order
	omegaNatural := make([]*big.Int, n)
	omegaNatural[0] = big.NewInt(1)
	for i := 1; i < n; i++ {
		omegaNatural[i] = new(big.Int).Mul(omegaNatural[i-1], root4096)
		omegaNatural[i].Mod(omegaNatural[i], mod)
	}

	// Apply bit-reversal permutation to match c-kzg-4844's brp_roots_of_unity
	omega := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		brpIdx := bitReverse(i, 12) // bitReverse(i, 12) same as bitReverse12(uint32(i))
		omega[i] = omegaNatural[brpIdx]
	}

	if debug {
		fmt.Println("first 5 ω (BRP order):")
		for i := 0; i < 5; i++ {
			fmt.Printf("  ω[%d] = %s\n", i, omega[i].Text(16))
		}
	}

	//------------------------------------------------------------------//
	// 2. Short‑circuit if z is a domain point
	//------------------------------------------------------------------//
	for k, w := range omega {
		if w.Cmp(z) == 0 {
			// Return blob[k] directly (same as c-kzg: return poly[i])
			return modBig(blobCell(blob, k)), nil
		}
	}

	//------------------------------------------------------------------//
	// 3. Summation (matching passing bit-reversal test exactly)
	//------------------------------------------------------------------//

	// Compute sum: Σ blob[i] * ω[i] / (z - ω[i])
	sum := big.NewInt(0)
	for i := 0; i < n; i++ {
		d := modBig(blobCell(blob, i))
		if d.Sign() == 0 {
			continue // Skip zero values
		}

		diff := new(big.Int).Sub(z, omega[i])
		diff.Mod(diff, mod)

		if diff.Sign() == 0 {
			continue // Skip if z == omega[i] (should be handled by early exit)
		}

		invDiff := new(big.Int).ModInverse(diff, mod)
		term := new(big.Int).Mul(d, omega[i])
		term.Mul(term, invDiff).Mod(term, mod)

		sum.Add(sum, term).Mod(sum, mod)

		if debug && i < 4 {
			fmt.Printf("term i=%d : %s\n", i, term.Text(16))
		}
	}

	//------------------------------------------------------------------//
	// 4. Apply barycentric formula: divide by n first, then multiply by (z^n - 1)
	// This matches c-kzg-4844 exactly: fr_div(out, out, &tmp) then blst_fr_mul(out, out, &tmp)
	//------------------------------------------------------------------//

	// Step 1: divide sum by n
	nInv := new(big.Int).ModInverse(big.NewInt(n), mod)
	result := new(big.Int).Mul(sum, nInv)
	result.Mod(result, mod)

	// Step 2: multiply by (z^n - 1)
	zPow := new(big.Int).Exp(z, big.NewInt(n), mod)
	zPowMinus1 := new(big.Int).Sub(zPow, big.NewInt(1))
	zPowMinus1.Mod(zPowMinus1, mod)

	result.Mul(result, zPowMinus1).Mod(result, mod)

	if debug {
		fmt.Printf("Σ = %s\n", sum.Text(16))
		temp := new(big.Int).Mul(sum, nInv)
		temp.Mod(temp, mod)
		fmt.Printf("Σ/n = %s\n", temp.Text(16))
		fmt.Printf("z^n - 1 = %s\n", zPowMinus1.Text(16))
		fmt.Printf("final = %s\n", result.Text(16))
	}

	return result, nil
}

// Helpers --------------------------------------------------------------------

func blobCell(blob *kzg4844.Blob, i int) *big.Int {
	start := i * 32
	return new(big.Int).SetBytes(blob[start : start+32])
}
