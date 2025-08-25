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
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

type cspProofCircuit struct {
	Proof      CSPProof
	CensusRoot emulated.Element[sw_bn254.ScalarField]
	ProcessID  emulated.Element[sw_bn254.ScalarField]
	Address    emulated.Element[sw_bn254.ScalarField]
}

func (c *cspProofCircuit) Define(api frontend.API) error {
	api.AssertIsEqual(c.Proof.IsValid(api, c.CensusRoot, c.ProcessID, c.Address), 1)
	return nil
}

func TestCSPProofCircuit(t *testing.T) {
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	c := qt.New(t)

	csp, err := New(types.CensusOriginCSPEdDSABLS12377, nil)
	c.Assert(err, qt.IsNil)

	orgAddress := common.Address(util.RandomBytes(20))
	userAddress := common.Address(util.RandomBytes(20))

	processID := &types.ProcessID{
		Address: orgAddress,
		Nonce:   rand.Uint64(),
		ChainID: rand.Uint32(),
	}

	proof, err := csp.GenerateProof(processID, userAddress)
	c.Assert(err, qt.IsNil)

	gnarkProof, err := CensusProofToCSPProof(proof)
	c.Assert(err, qt.IsNil)

	hexPID := types.HexBytes(processID.Marshal())
	ffPID := hexPID.BigInt().ToFF(circuits.BallotProofCurve.ScalarField()).MathBigInt()
	hexAddress := types.HexBytes(userAddress.Bytes())
	ffAddress := hexAddress.BigInt().ToFF(circuits.BallotProofCurve.ScalarField()).MathBigInt()
	assignments := &cspProofCircuit{
		Proof:      *gnarkProof,
		CensusRoot: emulated.ValueOf[sw_bn254.ScalarField](proof.Root.BigInt().MathBigInt()),
		ProcessID:  emulated.ValueOf[sw_bn254.ScalarField](ffPID),
		Address:    emulated.ValueOf[sw_bn254.ScalarField](ffAddress),
	}
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&cspProofCircuit{}, assignments,
		test.WithCurves(circuits.VoteVerifierCurve),
		test.WithBackends(backend.GROTH16))
}
