package aggregatortest

import (
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/test"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

func TestAggregatorCircuit(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == "false" {
		t.Skip("skipping circuit tests...")
	}
	// inputs generation
	startTime := time.Now()
	_, placeholder, assignment := AggregatorInputsForTest(t, testutil.FixedProcessID(), types.CensusOriginMerkleTreeOffchainStaticV1, 3)
	log.DebugTime("aggregator inputs generation", startTime)
	// proving
	startTime = time.Now()
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(placeholder, assignment,
		test.WithCurves(params.AggregatorCurve), test.WithBackends(backend.GROTH16),
		test.WithProverOpts(aggregator.ProverOptions...))
	log.DebugTime("aggregator proving", startTime)
}
