package csp

import (
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/logger"
	"github.com/consensys/gnark/profile"
	"github.com/consensys/gnark/test"
	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/rs/zerolog"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

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

	assignments := &cspProofCircuit{
		Proof:      *gnarkProof,
		CensusRoot: proof.Root.BigInt().MathBigInt(),
		ProcessID:  processID.MathBigInt(),
		Address:    userAddress.Big(),
		Weight:     userWeight.MathBigInt(),
	}
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&cspProofCircuit{}, assignments,
		test.WithCurves(params.StateTransitionCurve),
		test.WithBackends(backend.GROTH16))

	p := profile.Start()
	startTime := time.Now()
	_, _ = frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &cspProofCircuit{})
	log.DebugTime("csp circuit compile", startTime)
	p.Stop()
	log.Infow("constraints", "constraints", p.NbConstraints())
}
