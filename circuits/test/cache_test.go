package circuitstest

import (
	"testing"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/spec/params"
)

type addCircuit struct {
	A frontend.Variable
	B frontend.Variable
	C frontend.Variable `gnark:",public"`
}

func (c *addCircuit) Define(api frontend.API) error {
	api.AssertIsEqual(api.Add(c.A, c.B), c.C)
	return nil
}

type mulCircuit struct {
	A frontend.Variable
	B frontend.Variable
	C frontend.Variable `gnark:",public"`
}

func (c *mulCircuit) Define(api frontend.API) error {
	api.AssertIsEqual(api.Mul(c.A, c.B), c.C)
	return nil
}

func TestConstraintSystemHash(t *testing.T) {
	c := qt.New(t)

	addCS, err := frontend.Compile(params.VoteVerifierCurve.ScalarField(), r1cs.NewBuilder, &addCircuit{})
	c.Assert(err, qt.IsNil)
	mulCS, err := frontend.Compile(params.VoteVerifierCurve.ScalarField(), r1cs.NewBuilder, &mulCircuit{})
	c.Assert(err, qt.IsNil)

	addHash1, err := circuits.HashConstraintSystem(addCS)
	c.Assert(err, qt.IsNil)
	addHash2, err := circuits.HashConstraintSystem(addCS)
	c.Assert(err, qt.IsNil)
	mulHash, err := circuits.HashConstraintSystem(mulCS)
	c.Assert(err, qt.IsNil)

	c.Assert(addHash1, qt.Not(qt.Equals), "")
	c.Assert(addHash1, qt.Equals, addHash2)
	c.Assert(addHash1, qt.Not(qt.Equals), mulHash)
}

func TestGenerateCacheKeySeparatesNamespaces(t *testing.T) {
	c := qt.New(t)

	cache := &CircuitCache{}
	processID := testutil.FixedProcessID()
	ccsHash := "same-ccs-hash"

	aggKey := cache.GenerateCacheKey(ccsHash, processID, "aggregator", 3)
	stateTransitionKey := cache.GenerateCacheKey(ccsHash, processID, "statetransition-test-aggregator", 3)

	c.Assert(aggKey, qt.Not(qt.Equals), stateTransitionKey)
}

func TestAggregatorCircuitCCSHash(t *testing.T) {
	c := qt.New(t)

	hash1, err := AggregatorCircuitCCSHash()
	c.Assert(err, qt.IsNil)
	hash2, err := AggregatorCircuitCCSHash()
	c.Assert(err, qt.IsNil)

	c.Assert(hash1, qt.Not(qt.Equals), "")
	c.Assert(hash1, qt.Equals, hash2)
}
