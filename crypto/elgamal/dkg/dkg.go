package dkg

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/crypto/ecc"
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
