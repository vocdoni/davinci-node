package statetransitiontest

import (
	"log"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/logger"
	"github.com/consensys/gnark/test"
	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/rs/zerolog"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
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
	processID := &types.ProcessID{
		Address: common.BytesToAddress(util.RandomBytes(20)),
		Nonce:   rand.Uint64(),
		ChainID: rand.Uint32(),
	}
	_, placeholder, assignments, err := StateTransitionInputsForTest(processID, 3)
	c.Assert(err, qt.IsNil)
	c.Logf("inputs generation took %s", time.Since(now).String())
	// proving
	now = time.Now()
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(placeholder, assignments,
		test.WithCurves(circuits.StateTransitionCurve), test.WithBackends(backend.GROTH16))
	c.Logf("proving took %s", time.Since(now).String())
}
