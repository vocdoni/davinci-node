package voteverifiertest

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	gpugroth16 "github.com/consensys/gnark/backend/accelerated/icicle/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/logger"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/rs/zerolog"
	"github.com/vocdoni/davinci-node/circuits"
	ballottest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
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
	processID := types.TestProcessID
	_, placeholder, assignments := VoteVerifierInputsForTest(t, []VoterTestData{
		{
			PrivKey: s,
			PubKey:  s.PublicKey,
			Address: s.Address(),
		},
	}, processID, types.CensusOriginMerkleTree)

	// compile the circuit
	startTime := time.Now()
	t.Logf("compiling circuit...")
	css, err := frontend.Compile(circuits.VoteVerifierCurve.ScalarField(), r1cs.NewBuilder, &placeholder)
	c.Assert(err, qt.IsNil)
	t.Logf("circuit compiled, tooks %s", time.Since(startTime).String())

	t.Logf("setting up...")
	pk, vk, err := gpugroth16.Setup(css)
	c.Assert(err, qt.IsNil)

	witness, err := frontend.NewWitness(&assignments[0], ecc.BLS12_377.ScalarField())
	c.Assert(err, qt.IsNil)

	startTime = time.Now()
	proof, err := gpugroth16.Prove(css, pk, witness)
	c.Assert(err, qt.IsNil)
	t.Logf("proof generated, tooks %s", time.Since(startTime).String())

	// verify proof
	t.Logf("verifying...")
	publicWitness, err := witness.Public()
	c.Assert(err, qt.IsNil)
	err = gpugroth16.Verify(proof, vk, publicWitness)
	c.Assert(err, qt.IsNil)

	// Assert
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
	processID := types.TestProcessID
	_, placeholder, assignments := VoteVerifierInputsForTest(t, []VoterTestData{
		{
			PrivKey: s,
			PubKey:  s.PublicKey,
			Address: s.Address(),
		},
	}, processID, types.CensusOriginCSPEdDSABLS12377)
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
	placeholder, err := voteverifier.DummyPlaceholder(ballottest.TestCircomVerificationKey)
	c.Assert(err, qt.IsNil)
	assignment, err := voteverifier.DummyAssignment(ballottest.TestCircomVerificationKey, new(bjj.BJJ).New())
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
	processID := types.TestProcessID
	_, placeholder, assignments := VoteVerifierInputsForTest(t, data, processID, types.CensusOriginMerkleTree)
	assert := test.NewAssert(t)
	now := time.Now()
	for _, assignment := range assignments {
		assert.SolvingSucceeded(&placeholder, &assignment,
			test.WithCurves(ecc.BLS12_377),
			test.WithBackends(backend.GROTH16))
	}
	fmt.Println("proving tooks", time.Since(now))
}
