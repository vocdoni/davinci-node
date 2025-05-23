package dkg

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
)

// Participant represents a participant in the DKG protocol.
type Participant struct {
	ID             int
	Threshold      int
	Participants   []int
	SecretCoeffs   []*big.Int
	PublicCoeffs   []ecc.Point
	SecretShares   map[int]*big.Int
	ReceivedShares map[int]*big.Int
	PrivateShare   *big.Int
	PublicKey      ecc.Point
	CurvePoint     ecc.Point

	// added for distributed proof
	rRand       *big.Int             // own Schnorr randomness r_i
	Commitments map[int]CPCommitment // commitments received
	PartialZ    map[int]*big.Int     // z_i responses received
}

// CPCommitment represents the Schnorr commitment for a participant.
type CPCommitment struct { // per-participant Schnorr commitments
	A1, A2 ecc.Point
}

// NewParticipant initializes a new participant.
func NewParticipant(id int, threshold int, participants []int, curvePoint ecc.Point) *Participant {
	return &Participant{
		ID:             id,
		Threshold:      threshold,
		Participants:   participants,
		SecretCoeffs:   []*big.Int{},
		PublicCoeffs:   []ecc.Point{},
		SecretShares:   make(map[int]*big.Int),
		ReceivedShares: make(map[int]*big.Int),
		PrivateShare:   new(big.Int),
		CurvePoint:     curvePoint,
		Commitments:    make(map[int]CPCommitment),
		PartialZ:       make(map[int]*big.Int),
	}
}

func (p *Participant) GenerateSecretPolynomial() {
	degree := p.Threshold - 1

	// Generate random coefficients.
	for i := 0; i <= degree; i++ {
		coeff, err := rand.Int(rand.Reader, p.CurvePoint.Order())
		if err != nil {
			panic(err)
		}
		p.SecretCoeffs = append(p.SecretCoeffs, coeff)

		// Compute commitment G1 generator * coeff.
		commitment := p.CurvePoint.New()
		commitment.SetGenerator() // Set the generator
		commitment.ScalarMult(commitment, coeff)
		p.PublicCoeffs = append(p.PublicCoeffs, commitment)

		// Log the secret coefficient and commitment
		// log.Printf("Participant %d: SecretCoeff[%d] = %s", p.ID, i, coeff.String())
		// log.Printf("Participant %d: PublicCoeff[%d] = %s", p.ID, i, commitment.String())
	}
}

// ComputeShares computes shares to send to other participants.
func (p *Participant) ComputeShares() {
	for _, pid := range p.Participants {
		// Evaluate the polynomial at x = pid.
		share := p.evaluatePolynomial(big.NewInt(int64(pid)))
		p.SecretShares[pid] = share
	}
}

// evaluatePolynomial evaluates the secret polynomial at a given x.
func (p *Participant) evaluatePolynomial(x *big.Int) *big.Int {
	result := big.NewInt(0)
	xPower := big.NewInt(1)
	order := p.CurvePoint.Order()
	for _, coeff := range p.SecretCoeffs {
		term := new(big.Int).Mul(coeff, xPower)
		term.Mod(term, order)
		result.Add(result, term)
		result.Mod(result, order)

		xPower.Mul(xPower, x)
		xPower.Mod(xPower, order)
	}
	return result
}

// ReceiveShare receives a share from another participant.
func (p *Participant) ReceiveShare(fromID int, share *big.Int, publicCoeffs []ecc.Point) error {
	// Verify the share using the commitments.
	if !p.verifyShare(share, publicCoeffs) {
		return fmt.Errorf("invalid share from participant %d: %s", fromID, share.String())
	}
	p.ReceivedShares[fromID] = share
	return nil
}

// verifyShare verifies a received share using the commitments.
func (p *Participant) verifyShare(share *big.Int, publicCoeffs []ecc.Point) bool {
	// Compute lhs = G * share
	lhs := p.CurvePoint.New()
	lhs.ScalarBaseMult(share)

	// Compute rhs = sum_{i} publicCoeffs[i] * x^{i}
	rhs := p.CurvePoint.New()
	x := big.NewInt(int64(p.ID))
	xPower := big.NewInt(1)

	for _, coeffCommitment := range publicCoeffs {
		term := p.CurvePoint.New()
		term.ScalarMult(coeffCommitment, xPower)
		rhs.Add(rhs, term)

		xPower.Mul(xPower, x)
		// xPower.Mod(xPower, order) // Ensure this is removed
	}

	return lhs.Equal(rhs)
}

// AggregateShares aggregates the received shares to compute the private share.
func (p *Participant) AggregateShares() {
	order := p.CurvePoint.Order()
	p.PrivateShare.Set(p.SecretShares[p.ID])
	for _, share := range p.ReceivedShares {
		p.PrivateShare.Add(p.PrivateShare, share)
		p.PrivateShare.Mod(p.PrivateShare, order)
	}
}

// AggregatePublicKey aggregates the public commitments to compute the public key.
func (p *Participant) AggregatePublicKey(allPublicCoeffs map[int][]ecc.Point) {
	pk := p.CurvePoint.New()
	for _, coeffs := range allPublicCoeffs {
		pk.Add(pk, coeffs[0]) // Only the constant term is needed
	}
	p.PublicKey = pk
}

// BuildCommitment generates (A1_i , A2_i , r_i).
func (p *Participant) BuildCommitment(c1 ecc.Point) (CPCommitment, error) {
	order := c1.Order()
	r, err := rand.Int(rand.Reader, order)
	if err != nil {
		return CPCommitment{}, err
	}
	p.rRand = r

	A1 := c1.New()
	A1.ScalarBaseMult(r) // r·G
	A2 := c1.New()
	A2.ScalarMult(c1, r) // r·C1

	commit := CPCommitment{A1: A1, A2: A2}
	p.Commitments[p.ID] = commit // store own commitment
	return commit, nil
}

// ReceiveCommitment stores commitment from another participant.
func (p *Participant) ReceiveCommitment(id int, commit CPCommitment) {
	p.Commitments[id] = commit
}

// BuildPartialResponse returns z_i = r_i + e·λ_i·d_i  (mod order).
// caller passes:
//   - e       – challenge hash (same for all)
//   - lambda  – Lagrange coefficient λ_i  already computed externally
func (p *Participant) BuildPartialResponse(
	e, lambda *big.Int, order *big.Int,
) *big.Int {

	z := new(big.Int).Mul(lambda, p.PrivateShare) // λ_i·d_i
	z.Mul(z, e)                                   // e·λ_i·d_i
	z.Add(z, p.rRand)                             // r_i + …
	z.Mod(z, order)
	p.PartialZ[p.ID] = z
	return z
}

// ReceivePartialResponse stores z_j from another participant.
func (p *Participant) ReceivePartialResponse(id int, z *big.Int) {
	p.PartialZ[id] = z
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
