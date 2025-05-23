package dkg

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
)

// ComputePartialDecryption computes the partial decryption using the participant's private share.
func (p *Participant) ComputePartialDecryption(c1 ecc.Point) ecc.Point {
	// Compute s_i = privateShare * C1.
	si := c1.New()
	si.ScalarMult(c1, p.PrivateShare)
	return si
}

// CombinePartialDecryptions combines partial decryptions to recover the message.
func CombinePartialDecryptions(c2 ecc.Point, partialDecryptions map[int]ecc.Point, participants []int, maxMessage uint64) (*big.Int, error) {
	// Compute Lagrange coefficients.
	lagrangeCoeffs, err := computeLagrangeCoefficients(participants, c2.Order())
	if err != nil {
		return nil, fmt.Errorf("failed to compute Lagrange coefficients: %w", err)
	}

	// Sum up the partial decryptions weighted by Lagrange coefficients.
	s := c2.New()
	for _, id := range participants {
		pd := partialDecryptions[id]
		lambda := lagrangeCoeffs[id]
		term := s.New()
		term.ScalarMult(pd, lambda)
		s.Add(s, term)
	}
	// Compute M = C2 - s.
	s.Neg(s)
	m := c2.New()
	m.Add(c2, s)

	// Recover message scalar from point M using the elgamal package's implementation
	G := c2.New()
	G.SetGenerator()
	messageScalar, err := elgamal.BabyStepGiantStepECC(m, G, maxMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt message: %v", err)
	}

	return messageScalar, nil
}

// computeLagrangeCoefficients computes Lagrange coefficients for given participant IDs.
func computeLagrangeCoefficients(participants []int, mod *big.Int) (map[int]*big.Int, error) {
	coeffs := make(map[int]*big.Int)
	for _, i := range participants {
		numerator := big.NewInt(1)
		denominator := big.NewInt(1)
		for _, j := range participants {
			if i != j {
				// numerator *= -j mod mod
				tempNum := big.NewInt(int64(-j))
				tempNum.Mod(tempNum, mod)
				numerator.Mul(numerator, tempNum)
				numerator.Mod(numerator, mod)

				// denominator *= (i - j) mod mod
				tempDen := big.NewInt(int64(i - j))
				if tempDen.Sign() < 0 {
					tempDen.Add(tempDen, mod)
				}
				tempDen.Mod(tempDen, mod)
				denominator.Mul(denominator, tempDen)
				denominator.Mod(denominator, mod)
			}
		}
		denominatorInv := new(big.Int).ModInverse(denominator, mod)
		if denominatorInv == nil {
			return nil, fmt.Errorf("modular inverse does not exist for denominator %s modulo %s", denominator.String(), mod.String())
		}
		coeff := new(big.Int).Mul(numerator, denominatorInv)
		coeff.Mod(coeff, mod)
		coeffs[i] = coeff
	}
	return coeffs, nil
}
