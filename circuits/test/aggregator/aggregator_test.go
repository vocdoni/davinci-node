package aggregatortest

import (
	"log"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark/backend"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/consensys/gnark/test"
	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

func TestAggregatorCircuit(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
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
	_, placeholder, assignments, err := AggregatorInputsForTest(processID, 3)
	c.Assert(err, qt.IsNil)
	c.Logf("inputs generation tooks %s", time.Since(now).String())
	// proving
	now = time.Now()
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(placeholder, assignments,
		test.WithCurves(circuits.AggregatorCurve), test.WithBackends(backend.GROTH16),
		test.WithProverOpts(stdgroth16.GetNativeProverOptions(
			circuits.StateTransitionCurve.ScalarField(),
			circuits.AggregatorCurve.ScalarField())))
	c.Logf("proving tooks %s", time.Since(now).String())
}
