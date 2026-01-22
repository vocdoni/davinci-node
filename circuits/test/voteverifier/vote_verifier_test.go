package voteverifiertest

import (
	"fmt"
	"log"
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
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	ballottest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

func TestVerifyMerkletreeVoteCircuit(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	c := qt.New(t)
	// generate deterministic voter account for consistent caching
	s, err := ballottest.GenDeterministicECDSAaccountForTest(0)
	c.Assert(err, qt.IsNil)
	// Use centralized testing ProcessID for consistent caching
	_, placeholder, assignments := VoteVerifierInputsForTest(t, []VoterTestData{
		{
			PrivKey: s,
			PubKey:  s.PublicKey,
			Address: s.Address(),
		},
	}, testutil.FixedProcessID(), types.CensusOriginMerkleTreeOffchainStaticV1)
	// generate proof
	assert := test.NewAssert(t)
	now := time.Now()
	assert.SolvingSucceeded(&placeholder, &assignments[0],
		test.WithCurves(ecc.BLS12_377),
		test.WithBackends(backend.GROTH16))
	fmt.Println("proving tooks", time.Since(now))
}

func TestVerifyCSPVoteCircuit(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	c := qt.New(t)
	// generate deterministic voter account for consistent caching
	s, err := ballottest.GenDeterministicECDSAaccountForTest(0)
	c.Assert(err, qt.IsNil)
	// Use centralized testing ProcessID for consistent caching
	_, placeholder, assignments := VoteVerifierInputsForTest(t, []VoterTestData{
		{
			PrivKey: s,
			PubKey:  s.PublicKey,
			Address: s.Address(),
		},
	}, testutil.FixedProcessID(), types.CensusOriginCSPEdDSABabyJubJubV1)
	// generate proof
	assert := test.NewAssert(t)
	now := time.Now()
	assert.SolvingSucceeded(&placeholder, &assignments[0],
		test.WithCurves(ecc.BLS12_377),
		test.WithBackends(backend.GROTH16))
	fmt.Println("proving tooks", time.Since(now))
}

func TestVerifyNoValidVoteCircuit(t *testing.T) {
	c := qt.New(t)
	placeholder, err := voteverifier.DummyPlaceholder(ballotproof.CircomVerificationKey)
	c.Assert(err, qt.IsNil)
	assignment, err := voteverifier.DummyAssignment(ballotproof.CircomVerificationKey, new(bjj.BJJ).New())
	c.Assert(err, qt.IsNil)
	// generate proof
	assert := test.NewAssert(t)
	now := time.Now()
	assert.SolvingSucceeded(placeholder, assignment, test.WithCurves(ecc.BLS12_377), test.WithBackends(backend.GROTH16))
	fmt.Println("proving tooks", time.Since(now))
}

func TestVerifyMultipleVotesCircuit(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == "false" {
		t.Skip("skipping circuit tests...")
	}
	c := qt.New(t)
	data := []VoterTestData{}
	for i := range 10 {
		// generate deterministic voter account for consistent caching
		s, err := ballottest.GenDeterministicECDSAaccountForTest(i)
		c.Assert(err, qt.IsNil)
		data = append(data, VoterTestData{s, s.PublicKey, s.Address()})
	}
	// Use centralized testing ProcessID for consistent caching
	_, placeholder, assignments := VoteVerifierInputsForTest(t, data, testutil.FixedProcessID(), types.CensusOriginMerkleTreeOffchainStaticV1)
	assert := test.NewAssert(t)
	now := time.Now()
	for _, assignment := range assignments {
		assert.SolvingSucceeded(&placeholder, &assignment,
			test.WithCurves(ecc.BLS12_377),
			test.WithBackends(backend.GROTH16))
	}
	fmt.Println("proving tooks", time.Since(now))
}

func TestCompileAndPrintConstraints(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == "false" {
		t.Skip("skipping circuit tests...")
	}
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	c := qt.New(t)
	// generate vote verifier circuit and inputs with deterministic ProcessID
	vvPlaceholder, err := voteverifier.DummyPlaceholder(ballotproof.CircomVerificationKey)
	c.Assert(err, qt.IsNil, qt.Commentf("create vote verifier placeholder"))

	vvCCS, err := frontend.Compile(params.VoteVerifierCurve.ScalarField(), r1cs.NewBuilder, vvPlaceholder)
	c.Assert(err, qt.IsNil, qt.Commentf("compile vote verifier circuit"))
	fmt.Printf("vote verifier constraints: %d\n", vvCCS.GetNbConstraints())
}
