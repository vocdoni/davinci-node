package dkg

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc"
)

// VerifyCombinedDecryption checks that the decrypted plaintext `msgScalar`
// is the correct result of threshold‐decrypting ciphertext (c1,c2) with the
// provided partial decryptions.
//
// Inputs (all public):
//   - c2                 – second El Gamal component (C₂)
//   - partialDecryptions – map[id] → Sᵢ   where Sᵢ = dᵢ · C₁
//   - participants       – slice of participant IDs included in the decryption
//   - msgScalar          – announced plaintext scalar m
//
// It recomputes the aggregate secret ECDH point
//
//	S = Σ λᵢ · Sᵢ
//
// using the same Lagrange coefficients λᵢ that CombinePartialDecryptions uses,
// derives
//
//	M = C₂ – S
//
// and finally checks
//
//	M ?= m · G
//
// Returns nil on success or an error otherwise.
func VerifyCombinedDecryption(
	c2 ecc.Point,
	partialDecryptions map[int]ecc.Point,
	participants []int,
	msgScalar *big.Int,
) error {

	if len(participants) == 0 {
		return fmt.Errorf("no participants supplied")
	}
	if len(partialDecryptions) != len(participants) {
		return fmt.Errorf("partialDecryptions map size does not match participants slice")
	}

	// 1. Compute Lagrange coefficients λᵢ  (same helper as CombinePartialDecryptions)
	lagCoeff, err := computeLagrangeCoefficients(participants, c2.Order())
	if err != nil {
		return fmt.Errorf("cannot compute Lagrange coefficients: %w", err)
	}

	// 2. Aggregate the partial decryptions  S = Σ λᵢ·Sᵢ
	S := c2.New()
	for _, id := range participants {
		Si, ok := partialDecryptions[id]
		if !ok {
			return fmt.Errorf("missing partial decryption for participant %d", id)
		}
		lambda := lagCoeff[id]

		term := S.New()
		term.ScalarMult(Si, lambda)
		S.Add(S, term)
	}

	// 3. M = C₂ – S
	S.Neg(S)
	M := c2.New()
	M.Add(c2, S)

	// 4. Re‐encode the announced plaintext scalar  m·G
	msgScalar.Mod(msgScalar, c2.Order())
	Mexpected := c2.New()
	Mexpected.ScalarBaseMult(msgScalar) // m·G

	// 5. Compare
	if !M.Equal(Mexpected) {
		return fmt.Errorf("invalid decryption: reconstructed M != m·G")
	}
	return nil
}
