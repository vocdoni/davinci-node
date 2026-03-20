package elgamal

import (
	"testing"

	qt "github.com/frankban/quicktest"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
)

func TestBallotFromRTEtoTEInvalidCurve(t *testing.T) {
	c := qt.New(t)

	b := NewBallot(curves.New("bjj_gnark"))
	b.CurveType = "invalid_curve"

	converted := b.FromRTEtoTE()
	c.Assert(converted, qt.IsNil)
}

func TestBallotValidRejectsCiphertextsWithNilPoints(t *testing.T) {
	c := qt.New(t)

	ballot := NewBallot(curves.New(bjj.CurveType))
	for i := range ballot.Ciphertexts {
		ballot.Ciphertexts[i].C1 = nil
		ballot.Ciphertexts[i].C2 = nil
	}

	c.Assert(ballot.Valid(), qt.IsFalse)
}
