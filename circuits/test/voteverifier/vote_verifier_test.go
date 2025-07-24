package voteverifiertest

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/logger"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/rs/zerolog"
	ballottest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
)

func TestVerifySingleVoteCircuit(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	c := qt.New(t)
	// generate voter account
	s, err := ballottest.GenECDSAaccountForTest()
	c.Assert(err, qt.IsNil)
	_, placeholder, assignments, err := VoteVerifierInputsForTest([]VoterTestData{
		{
			PrivKey: s,
			PubKey:  s.PublicKey,
			Address: s.Address(),
		},
	}, nil)
	c.Assert(err, qt.IsNil)
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
	for range 10 {
		// generate voter account
		s, err := ballottest.GenECDSAaccountForTest()
		c.Assert(err, qt.IsNil)
		data = append(data, VoterTestData{s, s.PublicKey, s.Address()})
	}
	_, placeholder, assignments, err := VoteVerifierInputsForTest(data, nil)
	c.Assert(err, qt.IsNil)
	assert := test.NewAssert(t)
	now := time.Now()
	for _, assignment := range assignments {
		assert.SolvingSucceeded(&placeholder, &assignment,
			test.WithCurves(ecc.BLS12_377),
			test.WithBackends(backend.GROTH16))
	}
	fmt.Println("proving tooks", time.Since(now))
}
