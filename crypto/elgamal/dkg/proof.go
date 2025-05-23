package dkg

import (
	"crypto/rand"
	"math/big"

	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
)

// BuildCommitment creates the Schnorr commitment (A1,A2) and the nonce r.
func (p *Participant) BuildCommitment(c1 ecc.Point) (CPCommitment, *big.Int, error) {
	order := c1.Order()
	r, err := rand.Int(rand.Reader, order)
	if err != nil {
		return CPCommitment{}, nil, err
	}
	A1 := c1.New()
	A1.ScalarBaseMult(r) // r·G
	A2 := c1.New()
	A2.ScalarMult(c1, r) // r·C1
	return CPCommitment{A1: A1, A2: A2}, r, nil
}

// BuildPartialResponse computes z_i given λ_i, challenge e and nonce r.
func (p *Participant) BuildPartialResponse(
	r, lambda, e, order *big.Int,
) *big.Int {
	z := new(big.Int).Mul(lambda, p.PrivateShare) // λ_i·d_i
	z.Mul(z, e)                                   // e·λ_i·d_i
	z.Add(z, r)                                   // r + …
	z.Mod(z, order)
	return z
}

// AssembleDecryptionProof sums commitments & z-values and spits out
// a Chaum–Pedersen proof compatible with elgamal.VerifyDecryptionProof().
func AssembleDecryptionProof(
	publicKey ecc.Point,
	c1, c2 ecc.Point,
	msgScalar *big.Int, // plaintext scalar m
	commitments map[int]CPCommitment,
	partZ map[int]*big.Int,
) (*elgamal.DecryptionProof, error) {

	sumA1 := publicKey.New()
	sumA1.SetZero()
	sumA2 := publicKey.New()
	sumA2.SetZero()

	for _, com := range commitments {
		sumA1.Add(sumA1, com.A1)
		sumA2.Add(sumA2, com.A2)
	}

	zSum := new(big.Int)
	for _, z := range partZ {
		zSum.Add(zSum, z)
	}
	zSum.Mod(zSum, publicKey.Order())

	return &elgamal.DecryptionProof{A1: sumA1, A2: sumA2, Z: zSum}, nil
}
