package voteverifiertest

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	ballottest "github.com/vocdoni/vocdoni-z-sandbox/circuits/test/ballotproof"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/voteverifier"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
)

func TestVerifySingleVoteCircuit(t *testing.T) {
	c := qt.New(t)
	// generate voter account
	privKey, pubKey, address, err := ballottest.GenECDSAaccountForTest()
	c.Assert(err, qt.IsNil)
	_, placeholder, assignments, err := VoteVerifierInputsForTest([]VoterTestData{
		{
			PrivKey: privKey,
			PubKey:  pubKey,
			Address: address,
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
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == "false" {
		t.Skip("skipping circuit tests...")
	}
	c := qt.New(t)
	data := []VoterTestData{}
	for range 10 {
		// generate voter account
		privKey, pubKey, address, err := ballottest.GenECDSAaccountForTest()
		c.Assert(err, qt.IsNil)
		data = append(data, VoterTestData{privKey, pubKey, address})
	}
	_, placeholder, assignments, err := VoteVerifierInputsForTest(data, nil)
	c.Assert(err, qt.IsNil)
	assert := test.NewAssert(t)
	now := time.Now()
	for i, assignment := range assignments {
		c.Logf("proof %d of %d", i+1, len(assignments))
		err := test.IsSolved(&placeholder, &assignment, ecc.BLS12_377.ScalarField())
		assert.NoError(err)
	}
	fmt.Println("proving tooks", time.Since(now))
}
