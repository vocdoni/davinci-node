package csp

import (
	"math/rand"
	"os"
	"testing"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/logger"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/test"
	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/rs/zerolog"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
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
	api.AssertIsEqual(c.Proof.IsValidEmulated(api, types.CensusOriginCSPEdDSABN254V1.CurveID(), c.CensusRoot, c.ProcessID, c.Address, c.Weight), 1)
	return nil
}

func TestEmulatedCSPProofCircuit(t *testing.T) {
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	c := qt.New(t)

	csp, err := New(types.CensusOriginCSPEdDSABN254V1, nil)
	c.Assert(err, qt.IsNil)

	orgAddress := common.Address(util.RandomBytes(20))
	userAddress := common.Address(util.RandomBytes(20))
	userWeight := new(types.BigInt).SetInt(42)

	processID := &types.ProcessID{
		Address: orgAddress,
		Nonce:   rand.Uint64(),
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	proof, err := csp.GenerateProof(processID, userAddress, userWeight)
	c.Assert(err, qt.IsNil)

	gnarkProof, err := CensusProofToCSPProof(types.CensusOriginCSPEdDSABN254V1.CurveID(), proof)
	c.Assert(err, qt.IsNil)

	hexPID := types.HexBytes(processID.Marshal())
	ffPID := hexPID.BigInt().ToFF(params.BallotProofCurve.ScalarField()).MathBigInt()
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
	api.AssertIsEqual(c.Proof.IsValid(api, types.CensusOriginCSPEdDSABN254V1.CurveID(), c.CensusRoot, c.ProcessID, c.Address, c.Weight), 1)
	return nil
}

func TestCSPProofCircuit(t *testing.T) {
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	c := qt.New(t)

	csp, err := New(types.CensusOriginCSPEdDSABN254V1, nil)
	c.Assert(err, qt.IsNil)

	orgAddress := common.Address(util.RandomBytes(20))
	userAddress := common.Address(util.RandomBytes(20))
	userWeight := new(types.BigInt).SetInt(42)

	processID := &types.ProcessID{
		Address: orgAddress,
		Nonce:   rand.Uint64(),
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	proof, err := csp.GenerateProof(processID, userAddress, userWeight)
	c.Assert(err, qt.IsNil)

	gnarkProof, err := CensusProofToCSPProof(types.CensusOriginCSPEdDSABN254V1.CurveID(), proof)
	c.Assert(err, qt.IsNil)

	hexPID := types.HexBytes(processID.Marshal())
	ffPID := hexPID.BigInt().ToFF(params.BallotProofCurve.ScalarField()).MathBigInt()
	hexAddress := types.HexBytes(userAddress.Bytes())
	ffAddress := hexAddress.BigInt().ToFF(params.BallotProofCurve.ScalarField()).MathBigInt()
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
}
