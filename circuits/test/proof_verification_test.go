package circuitstest

import (
	"testing"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/prover"
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

func TestProveAndVerifyWithWitness(t *testing.T) {
	c := qt.New(t)

	ccs, err := frontend.Compile(params.VoteVerifierCurve.ScalarField(), r1cs.NewBuilder, &addCircuit{})
	c.Assert(err, qt.IsNil)

	pk, vk, err := prover.Setup(ccs)
	c.Assert(err, qt.IsNil)

	fullWitness, err := frontend.NewWitness(&addCircuit{A: 2, B: 3, C: 5}, params.VoteVerifierCurve.ScalarField())
	c.Assert(err, qt.IsNil)

	proof, err := ProveAndVerifyWithWitness(params.VoteVerifierCurve, ccs, pk, vk, fullWitness, nil, nil)
	c.Assert(err, qt.IsNil)
	c.Assert(proof, qt.Not(qt.IsNil))
}

func TestVerifyProofWithWitnessRejectsWrongVerifier(t *testing.T) {
	c := qt.New(t)

	ccs, err := frontend.Compile(params.VoteVerifierCurve.ScalarField(), r1cs.NewBuilder, &addCircuit{})
	c.Assert(err, qt.IsNil)

	pk, _, err := prover.Setup(ccs)
	c.Assert(err, qt.IsNil)

	wrongCCS, err := frontend.Compile(params.VoteVerifierCurve.ScalarField(), r1cs.NewBuilder, &mulCircuit{})
	c.Assert(err, qt.IsNil)

	_, wrongVK, err := prover.Setup(wrongCCS)
	c.Assert(err, qt.IsNil)

	fullWitness, err := frontend.NewWitness(&addCircuit{A: 2, B: 3, C: 5}, params.VoteVerifierCurve.ScalarField())
	c.Assert(err, qt.IsNil)

	proof, err := groth16.Prove(ccs, pk, fullWitness)
	c.Assert(err, qt.IsNil)

	err = VerifyProofWithWitness(proof, wrongVK, fullWitness)
	c.Assert(err, qt.Not(qt.IsNil))
}

func TestVerifyProofWithWitnessRejectsInvalidPublicWitness(t *testing.T) {
	c := qt.New(t)

	ccs, err := frontend.Compile(params.VoteVerifierCurve.ScalarField(), r1cs.NewBuilder, &addCircuit{})
	c.Assert(err, qt.IsNil)

	pk, vk, err := prover.Setup(ccs)
	c.Assert(err, qt.IsNil)

	fullWitness, err := frontend.NewWitness(&addCircuit{A: 2, B: 3, C: 5}, params.VoteVerifierCurve.ScalarField())
	c.Assert(err, qt.IsNil)

	proof, err := groth16.Prove(ccs, pk, fullWitness)
	c.Assert(err, qt.IsNil)

	wrongWitness, err := frontend.NewWitness(&addCircuit{A: 2, B: 3, C: 6}, params.VoteVerifierCurve.ScalarField())
	c.Assert(err, qt.IsNil)

	err = VerifyProofWithWitness(proof, vk, wrongWitness)
	c.Assert(err, qt.Not(qt.IsNil))
}
