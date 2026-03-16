package voteverifiertest

import (
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/logger"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/rs/zerolog"
	ballottest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

func TestVerifyMerkletreeVoteCircuit(t *testing.T) {
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	c := qt.New(t)
	// Generate a deterministic voter account for reproducible test data.
	s, err := ballottest.GenDeterministicECDSAaccountForTest(0)
	c.Assert(err, qt.IsNil)
	// Use a fixed ProcessID for reproducible test data.
	_, placeholder, assignments := VoteVerifierInputsForTest(t, []VoterTestData{
		{
			PrivKey: s,
			PubKey:  s.PublicKey,
			Address: s.Address(),
		},
	}, testutil.FixedProcessID(), types.CensusOriginMerkleTreeOffchainStaticV1)
	// generate proof
	assert := test.NewAssert(t)
	startTime := time.Now()
	assert.SolvingSucceeded(&placeholder, &assignments[0],
		test.WithCurves(ecc.BLS12_377),
		test.WithBackends(backend.GROTH16))
	log.DebugTime("vote verifier proving", startTime)
}

func TestVerifyCSPVoteCircuit(t *testing.T) {
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	c := qt.New(t)
	// Generate a deterministic voter account for reproducible test data.
	s, err := ballottest.GenDeterministicECDSAaccountForTest(0)
	c.Assert(err, qt.IsNil)
	// Use a fixed ProcessID for reproducible test data.
	_, placeholder, assignments := VoteVerifierInputsForTest(t, []VoterTestData{
		{
			PrivKey: s,
			PubKey:  s.PublicKey,
			Address: s.Address(),
		},
	}, testutil.FixedProcessID(), types.CensusOriginCSPEdDSABabyJubJubV1)
	// generate proof
	assert := test.NewAssert(t)
	startTime := time.Now()
	assert.SolvingSucceeded(&placeholder, &assignments[0],
		test.WithCurves(ecc.BLS12_377),
		test.WithBackends(backend.GROTH16))
	log.DebugTime("vote verifier proving", startTime)
}

func TestVerifyNoValidVoteCircuit(t *testing.T) {
	c := qt.New(t)
	placeholder, err := voteverifier.DummyPlaceholder()
	c.Assert(err, qt.IsNil)
	assignment, err := voteverifier.DummyAssignment()
	c.Assert(err, qt.IsNil)
	// generate proof
	assert := test.NewAssert(t)
	startTime := time.Now()
	assert.SolvingSucceeded(placeholder, assignment, test.WithCurves(ecc.BLS12_377), test.WithBackends(backend.GROTH16))
	log.DebugTime("vote verifier proving", startTime)
}

func TestVerifyMultipleVotesCircuit(t *testing.T) {
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == "false" {
		t.Skip("skipping circuit tests...")
	}
	c := qt.New(t)
	data := []VoterTestData{}
	for i := range 10 {
		// Generate a deterministic voter account for reproducible test data.
		s, err := ballottest.GenDeterministicECDSAaccountForTest(i)
		c.Assert(err, qt.IsNil)
		data = append(data, VoterTestData{s, s.PublicKey, s.Address()})
	}
	// Use a fixed ProcessID for reproducible test data.
	_, placeholder, assignments := VoteVerifierInputsForTest(t, data, testutil.FixedProcessID(), types.CensusOriginMerkleTreeOffchainStaticV1)
	assert := test.NewAssert(t)
	startTime := time.Now()
	for _, assignment := range assignments {
		assert.SolvingSucceeded(&placeholder, &assignment,
			test.WithCurves(ecc.BLS12_377),
			test.WithBackends(backend.GROTH16))
	}
	log.DebugTime("vote verifier batch proving", startTime)
}

func TestCompileAndPrintConstraints(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == "false" {
		t.Skip("skipping circuit tests...")
	}
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	c := qt.New(t)
	// generate vote verifier circuit and inputs with deterministic ProcessID
	vvPlaceholder, err := voteverifier.DummyPlaceholder()
	c.Assert(err, qt.IsNil, qt.Commentf("create vote verifier placeholder"))

	vvCCS, err := frontend.Compile(params.VoteVerifierCurve.ScalarField(), r1cs.NewBuilder, vvPlaceholder)
	c.Assert(err, qt.IsNil, qt.Commentf("compile vote verifier circuit"))
	log.Infow("vote verifier constraints", "constraints", vvCCS.GetNbConstraints())
}
