package statetransitiontest

import (
	"log"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/solidity"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/logger"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/rs/zerolog"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/types"
)

const falseString = "false"

func TestStateTransitionCircuit(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseString {
		t.Skip("skipping circuit tests...")
	}
	c := qt.New(t)
	// inputs generation
	now := time.Now()
	// Use centralized testing ProcessID for consistent caching
	processID := types.TestProcessID
	_, placeholder, assignments := StateTransitionInputsForTest(t, processID, types.CensusOriginMerkleTree, 3)
	c.Logf("inputs generation took %s", time.Since(now).String())
	// proving
	now = time.Now()

	assert := test.NewAssert(t)
	assert.SolvingSucceeded(placeholder, assignments,
		test.WithCurves(circuits.StateTransitionCurve), test.WithBackends(backend.GROTH16))
	c.Logf("proving took %s", time.Since(now).String())
}

func TestStateTransitionFullProvingCircuit(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	if os.Getenv("RUN_CIRCUIT_BENCHMARK") == "" || os.Getenv("RUN_CIRCUIT_BENCHMARK") == falseString {
		t.Skip("skipping full circuit tests...")
	}
	c := qt.New(t)
	// inputs generation
	totalStart := time.Now()
	now := time.Now()

	// Use centralized testing ProcessID for consistent caching
	processID := types.TestProcessID
	_, placeholder, assignments := StateTransitionInputsForTest(t, processID, types.CensusOriginMerkleTree, 3)
	c.Logf("inputs generation took %s", time.Since(now).String())

	// compile circuit
	now = time.Now()
	ccs, err := frontend.Compile(circuits.StateTransitionCurve.ScalarField(), r1cs.NewBuilder, placeholder)
	c.Assert(err, qt.IsNil, qt.Commentf("compile circuit"))
	c.Logf("compiled circuit with %d constraints, took %s", ccs.GetNbConstraints(), time.Since(now).String())

	// setup proving and verifying keys
	now = time.Now()
	pk, vk, err := prover.Setup(ccs)
	c.Assert(err, qt.IsNil, qt.Commentf("setup"))
	c.Logf("setup took %s", time.Since(now).String())

	// get number of iterations from environment variable
	iterations := 1
	if iterStr := os.Getenv("RUN_CIRCUIT_BENCHMARK"); iterStr != "" && iterStr != falseString {
		if n, err := strconv.Atoi(iterStr); err == nil && n > 0 {
			iterations = n
		}
	}
	// create witness once
	now = time.Now()
	w, err := frontend.NewWitness(assignments, circuits.StateTransitionCurve.ScalarField())
	c.Assert(err, qt.IsNil, qt.Commentf("create witness"))
	c.Logf("witness creation took %s", time.Since(now).String())

	c.Logf("running %d proving iterations", iterations)

	opts := solidity.WithProverTargetSolidityVerifier(backend.GROTH16)
	var proof groth16.Proof

	// loop over proving
	for i := 0; i < iterations; i++ {
		// prove using ProveWithWitness which supports GPU acceleration
		now = time.Now()
		proof, err = prover.ProveWithWitness(circuits.StateTransitionCurve, ccs, pk, w, opts)
		c.Assert(err, qt.IsNil, qt.Commentf("prove iteration %d", i+1))
		c.Logf("iteration %d: proving took %s", i+1, time.Since(now).String())
	}

	// verify the last proof
	now = time.Now()
	publicWitness, err := w.Public()
	c.Assert(err, qt.IsNil, qt.Commentf("get public witness"))
	err = groth16.Verify(proof, vk, publicWitness)
	c.Assert(err, qt.IsNil, qt.Commentf("verify proof"))
	c.Logf("verification took %s", time.Since(now).String())

	c.Logf("total proving process took %s", time.Since(totalStart).String())
}
