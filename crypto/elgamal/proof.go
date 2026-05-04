// Chaum-Pedersen NIZK proof of correct ElGamal decryption
//
// Context (refs):
//   - C. Pedersen & D. Chaum, “Wallet Databases with Observers” (1992)
//   - Helios e-voting scheme (https://doi.org/10.1007/978-3-642-12980-3_9)
//
// Goal: prove NON-interactively that a plaintext M is the correct decryption
// of ciphertext (C1, C2) under public key P = d·G, *without* revealing either
// the private key d or the encryption nonce k.
// We prove equality of discrete logs:
//
//	log_G(P) = log_{C1}(C2 – M·G)
//
// The Σ-protocol is rendered non-interactive with the Fiat–Shamir transform
// (hashing all public data to obtain the challenge).
//
// Public data
//   - G: group generator
//   - P: public key (d·G)
//   - C1 y C2: ciphertext
//   - M: plaintext
//
// Secret held by prover
//   - d: private key
//   - r: fresh random nonce
//   - k: encryption nonce (not revealed)
//
// Prover (BuildDecryptionProof):
//  1. Pick r ← 𝔽*.
//  2. A1 = r·G,  A2 = r·C1                (commitment)
//  3. D  = C2 – M·G                       (shared secret)
//  4. e  = H(G,P,C1,D,A1,A2) mod order    (Fiat-Shamir)
//  5. z  = r + e·d mod order              (response)
//
// Proof is (A1,A2,z).
//
// Verifier (VerifyDecryptionProof):
//
//	Recompute D and e, then check
//	    z·G  ==  A1 + e·P
//	    z·C1 ==  A2 + e·D
//
// Both must hold for the proof to be accepted.
package elgamal

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/crypto/hash/poseidon"
	"github.com/vocdoni/gnark-crypto-primitives/elgamal"
)

// DecryptionProof is a non-interactive Chaum–Pedersen proof that
// C2 – M·G and C1 share the same discrete log with respect to P and G.
type DecryptionProof struct {
	A1 ecc.Point // = r·G        (commitment wrt base G)
	A2 ecc.Point // = r·C1       (commitment wrt base C1)
	Z  *big.Int  // = r + e·d    (response)
}

// ToGnark converts the proof to the gnark format. This is used to verify the
// proof in a gnark circuit.
func (p *DecryptionProof) ToGnark() elgamal.DecryptionProof {
	a1X, a1Y := p.A1.Point()
	a2X, a2Y := p.A2.Point()
	return elgamal.DecryptionProof{
		A1: twistededwards.Point{
			X: a1X,
			Y: a1Y,
		},
		A2: twistededwards.Point{
			X: a2X,
			Y: a2Y,
		},
		Z: p.Z,
	}
}

// BuildDecryptionProof creates a Chaum–Pedersen NIZK proving that msg is the
// correct decryption of ciphertext (c1,c2) under privateKey.
func BuildDecryptionProof(
	privateKey *big.Int,
	publicKey ecc.Point,
	c1, c2 ecc.Point,
	msg *big.Int,
) (*DecryptionProof, error) {
	order := publicKey.Order()

	// 1. Sample fresh randomness r ∈ [1,order-1]
	r, err := rand.Int(rand.Reader, order)
	if err != nil {
		return nil, fmt.Errorf("failed to sample r: %v", err)
	}
	if r.Sign() == 0 { // reject 0
		r = big.NewInt(1)
	}

	// 2. Compute commitments A1 = r·G,  A2 = r·C1
	A1 := publicKey.New()
	A1.ScalarBaseMult(r) // r·G

	A2 := publicKey.New()
	A2.ScalarMult(c1, r) // r·C1

	// 3. Compute D = C2 – M·G  (shared secret part)
	m := new(big.Int).Mod(msg, order)
	M := publicKey.New()
	M.ScalarBaseMult(m) // M·G

	D := publicKey.New()
	D.Set(c2)
	negM := publicKey.New()
	negM.Neg(M)
	D.Add(D, negM) // D = C2 – M·G

	// 4. Fiat–Shamir challenge e = H(G,P,C1,D,A1,A2) mod order
	e, err := HashPointsToScalar(publicKey, // G is implicit in Point, but include for domain-sep
		publicKey, // P
		c1,
		D,
		A1,
		A2,
	)
	if err != nil {
		return nil, err
	}

	// 5. Response z = r + e·d mod order
	z := new(big.Int).Mul(e, privateKey)
	z.Add(z, r)
	z.Mod(z, order)

	return &DecryptionProof{A1: A1, A2: A2, Z: z}, nil
}

// VerifyDecryptionProof checks a Chaum–Pedersen proof of correct decryption.
// Returns nil if the proof is valid.
func VerifyDecryptionProof(
	publicKey ecc.Point,
	c1, c2 ecc.Point,
	msg *big.Int,
	proof *DecryptionProof,
) error {
	order := publicKey.Order()

	// Recompute D = C2 – M·G
	m := new(big.Int).Mod(msg, order)
	M := publicKey.New()
	M.ScalarBaseMult(m)

	D := publicKey.New()
	D.Set(c2)
	negM := publicKey.New()
	negM.Neg(M)
	D.Add(D, negM) // D = C2 – M·G

	// Recompute Fiat–Shamir challenge e
	e, err := HashPointsToScalar(publicKey, // G (domain separation)
		publicKey, // P
		c1,
		D,
		proof.A1,
		proof.A2,
	)
	if err != nil {
		return err
	}

	// Check 1:  z·G  ==  A1 + e·P
	left1 := publicKey.New()
	left1.ScalarBaseMult(proof.Z) // z·G

	right1 := publicKey.New()
	right1.Set(proof.A1)
	tmp := publicKey.New()
	tmp.ScalarMult(publicKey, e) // e·P
	right1.Add(right1, tmp)      // A1 + e·P

	if !left1.Equal(right1) {
		return fmt.Errorf("invalid proof: first equation fails")
	}

	// Check 2:  z·C1 ==  A2 + e·D
	left2 := publicKey.New()
	left2.ScalarMult(c1, proof.Z) // z·C1

	right2 := publicKey.New()
	right2.Set(proof.A2)
	tmp.ScalarMult(D, e) // reuse tmp : e·D
	right2.Add(right2, tmp)

	if !left2.Equal(right2) {
		return fmt.Errorf("invalid proof: second equation fails")
	}

	return nil
}

// Helper: hash a sequence of points to a scalar < order using Poseidon.
// This is the Fiat–Shamir transform.
func HashPointsToScalar(pts ...ecc.Point) (*big.Int, error) {
	points := []*big.Int{}
	for _, p := range pts {
		// ecc.Point.Marshal() must be deterministic.
		x, y := p.Point()
		points = append(points, x, y)
	}
	digest, err := poseidon.MultiPoseidon(points...)
	if err != nil {
		return nil, fmt.Errorf("failed to hash points: %w", err)
	}
	return digest, nil
}
