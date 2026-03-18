package aggregatortest

import (
	"os"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// TestAggregatorCircuitProve performs a full proof using the GPU prover (if enabled)
// to exercise the BW6-761 icicle backend. It compiles the aggregator circuit,
// generates a witness, proves, and verifies the proof end-to-end.
func TestAggregatorCircuitProve(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == "false" {
		t.Skip("skipping circuit tests...")
	}
	c := qt.New(t)

	processID := testutil.FixedProcessID()
	nValidVoters := 3

	_, placeholder, assignment := AggregatorInputsForTest(t, processID, types.CensusOriginMerkleTreeOffchainStaticV1, nValidVoters)
	aggregatorRuntime, err := aggregator.Artifacts.LoadOrSetupForCircuit(t.Context(), placeholder)
	c.Assert(err, qt.IsNil, qt.Commentf("resolve aggregator runtime artifacts"))

	// Prove and verify
	log.Infow("proving and verifying aggregator circuit")
	_, err = aggregatorRuntime.ProveAndVerify(assignment)
	c.Assert(err, qt.IsNil, qt.Commentf("prove aggregator circuit"))
}
