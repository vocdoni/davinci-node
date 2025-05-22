package dkg

import (
	"crypto/rand"
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_iden3"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/curves"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
)

func TestVerifyCombinedDecryption(t *testing.T) {
	const (
		maxValue  = 5
		numVoters = 25
		threshold = 3
	)
	c := qt.New(t)

	// 1.  A tiny DKG session
	curve := curves.New(bjj.CurveType)
	ids := []int{1, 2, 3, 4, 5}

	ps := map[int]*Participant{}
	for _, id := range ids {
		p := NewParticipant(id, threshold, ids, curve)
		p.GenerateSecretPolynomial()
		ps[id] = p
	}
	// commitments & shares
	comm := map[int][]ecc.Point{}
	for id, p := range ps {
		comm[id] = p.PublicCoeffs
		p.ComputeShares()
	}
	for _, p := range ps {
		for id, q := range ps {
			if p.ID != id {
				err := p.ReceiveShare(id, q.SecretShares[p.ID], q.PublicCoeffs)
				c.Assert(err, qt.IsNil)
			}
		}
		p.AggregateShares()
	}
	for _, p := range ps {
		p.AggregatePublicKey(comm)
	}
	pubKey := ps[1].PublicKey

	// 2.  Aggregate numVoters votes
	sum := big.NewInt(0)
	C1 := curve.New()
	C2 := curve.New()
	C1.SetZero()
	C2.SetZero()

	for range numVoters {
		v, _ := rand.Int(rand.Reader, big.NewInt(int64(maxValue)))
		sum.Add(sum, v)

		c1, c2, _, err := elgamal.Encrypt(pubKey, v)
		c.Assert(err, qt.IsNil)
		C1.SafeAdd(C1, c1)
		C2.SafeAdd(C2, c2)
	}

	// 3.  Threshold decrypt with {1,2,3}
	subset := []int{1, 2, 3}
	partials := map[int]ecc.Point{}
	for _, id := range subset {
		partials[id] = ps[id].ComputePartialDecryption(C1)
	}

	mScalar, err := CombinePartialDecryptions(C2, partials, subset,
		uint64(maxValue*numVoters)+1)
	c.Assert(err, qt.IsNil)
	c.Assert(mScalar.Cmp(sum), qt.Equals, 0)

	// 4.  Positive verification
	err = VerifyCombinedDecryption(C2, partials, subset, mScalar)
	c.Assert(err, qt.IsNil)

	// 5.  Negative cases
	// (a) Tamper with the message
	badMsg := new(big.Int).Add(mScalar, big.NewInt(1))
	err = VerifyCombinedDecryption(C2, partials, subset, badMsg)
	c.Assert(err, qt.Not(qt.IsNil))

	// (b) Tamper with a partial decryption
	badPartials := map[int]ecc.Point{}
	for id, pt := range partials {
		if id == subset[0] {
			badPartials[id] = curve.New() // obviously wrong point
			badPartials[id].SetZero()
		} else {
			badPartials[id] = pt
		}
	}
	err = VerifyCombinedDecryption(C2, badPartials, subset, mScalar)
	c.Assert(err, qt.Not(qt.IsNil))
}
