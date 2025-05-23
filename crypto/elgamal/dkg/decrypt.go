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

// CombinePartialDecryptionsWithProof combines the threshold partial decryptions,
// recovers the plaintext scalar **and** produces a Chaum–Pedersen proof that can
// be verified with the existing `elgamal.VerifyDecryptionProof`.
//
// Inputs (all already available to the tallying authority):
//   - c1, c2        – aggregate ciphertext
//   - publicKey     – aggregated public key  P = d·G
//   - partials      – map[id] → Sᵢ  where  Sᵢ = dᵢ·C1      (public data)
//   - shares        – map[id] → dᵢ                    (private shares)
//   - participants  – slice with the ids contained in the two maps
//   - maxMessage    – upper bound for discrete-log search (baby-step giant-step)
//
// Returns (messageScalar, proof, error).
func CombinePartialDecryptionsWithProof(
	c1, c2 ecc.Point,
	publicKey ecc.Point,
	partials map[int]ecc.Point,
	shares map[int]*big.Int,
	participants []int,
	maxMessage uint64,
) (*big.Int, *elgamal.DecryptionProof, error) {

	// 1.  Lagrange coefficients λᵢ
	lag, err := computeLagrangeCoefficients(participants, c1.Order())
	if err != nil {
		return nil, nil, fmt.Errorf("lagrange: %w", err)
	}

	// 2.  Aggregate the decryption share  S = Σ λᵢ·Sᵢ
	S := c2.New()
	S.SetZero()
	for _, id := range participants {
		si, ok := partials[id]
		if !ok {
			return nil, nil, fmt.Errorf("missing partial for id %d", id)
		}
		term := S.New()
		term.ScalarMult(si, lag[id])
		S.Add(S, term)
	}

	// 3.  Plain-text point  m = C2 – S
	mPt := c2.New()
	mPt.Set(c2)
	negS := S.New()
	negS.Neg(S)
	mPt.Add(mPt, negS) // m = C2 – S

	// 4.  Recover the plaintext scalar with baby-step giant-step
	G := c2.New()
	G.SetGenerator()
	mScalar, err := elgamal.BabyStepGiantStepECC(mPt, G, maxMessage)
	if err != nil {
		return nil, nil, fmt.Errorf("discrete-log: %w", err)
	}

	// 5.  Reconstruct the aggregate secret key  d = Σ λᵢ·dᵢ
	order := c1.Order()
	d := big.NewInt(0)
	for _, id := range participants {
		di, ok := shares[id]
		if !ok {
			return nil, nil, fmt.Errorf("missing secret share for id %d", id)
		}
		term := new(big.Int).Mul(lag[id], di)
		term.Mod(term, order)
		d.Add(d, term)
		d.Mod(d, order)
	}

	// 6.  Build the standard Chaum–Pedersen proof with that `d`
	proof, err := elgamal.BuildDecryptionProof(d, publicKey, c1, c2, mScalar)
	if err != nil {
		return nil, nil, err
	}

	return mScalar, proof, nil
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
