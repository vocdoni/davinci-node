package elgamal

import (
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
)

func TestBallotFromRTEtoTEInvalidCurve(t *testing.T) {
	c := qt.New(t)

	b := NewBallot(curves.New("bjj_gnark"))
	b.CurveType = "invalid_curve"

	converted := b.FromRTEtoTE()
	c.Assert(converted, qt.IsNil)
}
