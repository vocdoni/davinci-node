package statetransition

import (
	"math/big"
	"strings"
	"testing"

	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/log"
)

func TestSanitizeWitnessForDebug(t *testing.T) {
	c := qt.New(t)

	witness := &StateTransitionCircuit{
		RootHashBefore: big.NewInt(1),
	}
	witness.AggregatorVK.G1.K = make([]sw_bw6761.G1Affine, 1)

	sanitized := WitnessForDebug(witness)

	c.Assert(sanitized, qt.Not(qt.IsNil))
	c.Assert(witness.AggregatorVK.G1.K, qt.HasLen, 1)
	c.Assert(sanitized.AggregatorVK.G1.K, qt.HasLen, 0)

	sanitized.RootHashBefore = big.NewInt(2)
	c.Assert(witness.RootHashBefore.(*big.Int).Cmp(big.NewInt(1)), qt.Equals, 0)
}

func TestMarshalWitnessForDebugNil(t *testing.T) {
	c := qt.New(t)

	out, err := MarshalWitnessForDebug(nil)
	c.Assert(err, qt.ErrorMatches, "witness is nil")
	c.Assert(out, qt.IsNil)
}

func TestMarshalWitnessForDebug(t *testing.T) {
	c := qt.New(t)

	witness := &StateTransitionCircuit{
		RootHashBefore: big.NewInt(7),
	}
	witness.AggregatorVK.G1.K = make([]sw_bw6761.G1Affine, 2)

	out, err := MarshalWitnessForDebug(witness)
	c.Assert(err, qt.IsNil)
	c.Assert(len(out) > 0, qt.IsTrue)
	c.Assert(strings.Contains(string(out), "\"RootHashBefore\""), qt.IsTrue)
	c.Assert(witness.AggregatorVK.G1.K, qt.HasLen, 2)

	log.Info(string(out))
}
