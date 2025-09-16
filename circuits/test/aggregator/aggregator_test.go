package aggregatortest

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark/backend"
	gpugroth16 "github.com/consensys/gnark/backend/accelerated/icicle/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/logger"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/rs/zerolog"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/types"
)

func TestAggregatorCircuit(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == "false" {
		t.Skip("skipping circuit tests...")
	}
	c := qt.New(t)
	// inputs generation
	now := time.Now()
	// Use centralized testing ProcessID for consistent caching
	processID := types.TestProcessID
	_, placeholder, assignments := AggregatorInputsForTest(t, processID, 3)
	c.Logf("inputs generation tooks %s", time.Since(now).String())

	// compile the circuit
	startTime := time.Now()
	t.Logf("compiling circuit...")
	css, err := frontend.Compile(circuits.AggregatorCurve.ScalarField(), r1cs.NewBuilder, placeholder)
	c.Assert(err, qt.IsNil)
	t.Logf("circuit compiled, tooks %s", time.Since(startTime).String())

	t.Logf("setting up...")
	pk, vk, err := gpugroth16.Setup(css)
	c.Assert(err, qt.IsNil)

	witness, err := frontend.NewWitness(assignments, circuits.AggregatorCurve.ScalarField())
	c.Assert(err, qt.IsNil)

	startTime = time.Now()
	proof, err := gpugroth16.Prove(css, pk, witness, stdgroth16.GetNativeProverOptions(
		circuits.StateTransitionCurve.ScalarField(),
		circuits.AggregatorCurve.ScalarField()))
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
	now = time.Now()
	t.Logf("solving with debug traces...")
	assert.SolvingSucceeded(placeholder, assignments,
		test.WithCurves(circuits.AggregatorCurve), test.WithBackends(backend.GROTH16),
		test.WithProverOpts(stdgroth16.GetNativeProverOptions(
			circuits.StateTransitionCurve.ScalarField(),
			circuits.AggregatorCurve.ScalarField())))
	fmt.Println("solving tooks", time.Since(now))
}
