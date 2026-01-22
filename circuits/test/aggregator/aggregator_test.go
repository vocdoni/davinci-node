package aggregatortest

import (
	"log"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark/backend"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

func TestAggregatorCircuit(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == "false" {
		t.Skip("skipping circuit tests...")
	}
	c := qt.New(t)
	// inputs generation
	now := time.Now()
	_, placeholder, assignments := AggregatorInputsForTest(t, testutil.FixedProcessID(), types.CensusOriginMerkleTreeOffchainStaticV1, 3)
	c.Logf("inputs generation tooks %s", time.Since(now).String())
	// proving
	now = time.Now()
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(placeholder, assignments,
		test.WithCurves(params.AggregatorCurve), test.WithBackends(backend.GROTH16),
		test.WithProverOpts(stdgroth16.GetNativeProverOptions(
			params.StateTransitionCurve.ScalarField(),
			params.AggregatorCurve.ScalarField())))
	c.Logf("proving tooks %s", time.Since(now).String())
}
