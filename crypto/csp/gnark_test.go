package csp

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/logger"
	"github.com/consensys/gnark/profile"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/test"
	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/rs/zerolog"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

type emulatedCSPProofCircuit struct {
	Proof      CSPProof
	CensusRoot emulated.Element[sw_bn254.ScalarField]
	ProcessID  emulated.Element[sw_bn254.ScalarField]
	Address    emulated.Element[sw_bn254.ScalarField]
	Weight     emulated.Element[sw_bn254.ScalarField]
}

func (c *emulatedCSPProofCircuit) Define(api frontend.API) error {
	api.AssertIsEqual(c.Proof.IsValidEmulated(api, types.CensusOriginCSPEdDSABabyJubJubV1.CurveID(), c.CensusRoot, c.ProcessID, c.Address, c.Weight), 1)
	return nil
}

func TestEmulatedCSPProofCircuit(t *testing.T) {
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	c := qt.New(t)

	csp, err := New(types.CensusOriginCSPEdDSABabyJubJubV1, nil)
	c.Assert(err, qt.IsNil)

	processID := testutil.RandomProcessID()
	userAddress := testutil.RandomAddress()
	userWeight := types.NewInt(testutil.Weight)

	proof, err := csp.GenerateProof(processID, userAddress, userWeight)
	c.Assert(err, qt.IsNil)

	gnarkProof, err := CensusProofToCSPProof(types.CensusOriginCSPEdDSABabyJubJubV1.CurveID(), proof)
	c.Assert(err, qt.IsNil)

	ffPID := processID.BigInt().ToFF(params.BallotProofCurve.ScalarField()).MathBigInt()
	hexAddress := types.HexBytes(userAddress.Bytes())
	ffAddress := hexAddress.BigInt().ToFF(params.BallotProofCurve.ScalarField()).MathBigInt()
	ffWeight := userWeight.ToFF(params.BallotProofCurve.ScalarField()).MathBigInt()
	assignments := &emulatedCSPProofCircuit{
		Proof:      *gnarkProof,
		CensusRoot: emulated.ValueOf[sw_bn254.ScalarField](proof.Root.BigInt().MathBigInt()),
		ProcessID:  emulated.ValueOf[sw_bn254.ScalarField](ffPID),
		Address:    emulated.ValueOf[sw_bn254.ScalarField](ffAddress),
		Weight:     emulated.ValueOf[sw_bn254.ScalarField](ffWeight),
	}
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&emulatedCSPProofCircuit{}, assignments,
		test.WithCurves(params.StateTransitionCurve),
		test.WithBackends(backend.GROTH16))
}

type cspProofCircuit struct {
	Proof      CSPProof
	CensusRoot frontend.Variable
	ProcessID  frontend.Variable
	Address    frontend.Variable
	Weight     frontend.Variable
}

func (c *cspProofCircuit) Define(api frontend.API) error {
	api.AssertIsEqual(c.Proof.IsValid(api, c.CensusRoot, c.ProcessID, c.Address, c.Weight), 1)
	return nil
}

func TestCSPProofCircuit(t *testing.T) {
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	c := qt.New(t)

	csp, err := New(types.CensusOriginCSPEdDSABabyJubJubV1, nil)
	c.Assert(err, qt.IsNil)

	userAddress := common.Address(util.RandomBytes(20))
	userWeight := new(types.BigInt).SetInt(42)

	processID := testutil.RandomProcessID()

	proof, err := csp.GenerateProof(processID, userAddress, userWeight)
	c.Assert(err, qt.IsNil)

	gnarkProof, err := CensusProofToCSPProof(types.CensusOriginCSPEdDSABabyJubJubV1.CurveID(), proof)
	c.Assert(err, qt.IsNil)

	ffPID := types.BigIntConverter(processID.MathBigInt()).ToFF(params.BallotProofCurve.ScalarField()).MathBigInt()
	ffAddress := types.BigIntConverter(userAddress.Big()).ToFF(params.BallotProofCurve.ScalarField()).MathBigInt()
	ffWeight := userWeight.ToFF(params.BallotProofCurve.ScalarField()).MathBigInt()
	assignments := &cspProofCircuit{
		Proof:      *gnarkProof,
		CensusRoot: proof.Root.BigInt().MathBigInt(),
		ProcessID:  ffPID,
		Address:    ffAddress,
		Weight:     ffWeight,
	}
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&cspProofCircuit{}, assignments,
		test.WithCurves(params.StateTransitionCurve),
		test.WithBackends(backend.GROTH16))

	p := profile.Start()
	now := time.Now()
	_, _ = frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &cspProofCircuit{})
	fmt.Println("elapsed", time.Since(now))
	p.Stop()
	fmt.Println("constrains", p.NbConstraints())
}
