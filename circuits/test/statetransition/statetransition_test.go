package statetransitiontest

import (
	"log"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/logger"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/rs/zerolog"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/types"
)

func TestStateTransitionCircuit(t *testing.T) {
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
	_, placeholder, assignments := StateTransitionInputsForTest(t, processID, 3)
	c.Logf("inputs generation took %s", time.Since(now).String())
	// proving
	now = time.Now()
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(placeholder, assignments,
		test.WithCurves(circuits.StateTransitionCurve), test.WithBackends(backend.GROTH16))
	c.Logf("proving took %s", time.Since(now).String())
}
