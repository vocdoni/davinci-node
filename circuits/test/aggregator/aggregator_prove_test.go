package aggregatortest

import (
	"os"
	"testing"

	"github.com/consensys/gnark/frontend"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/spec/params"
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

	_, _, assignment := AggregatorInputsForTest(t, processID, types.CensusOriginMerkleTreeOffchainStaticV1, nValidVoters)

	aggregatorRuntime, err := aggregator.Artifacts.LoadOrDownload(t.Context())
	c.Assert(err, qt.IsNil, qt.Commentf("load aggregator runtime artifacts"))

	fullWitness, err := frontend.NewWitness(assignment, params.AggregatorCurve.ScalarField())
	c.Assert(err, qt.IsNil, qt.Commentf("witness creation"))

	// Prove and verify
	log.Infow("proving and verifying aggregator circuit")
	_, err = aggregatorRuntime.ProveAndVerifyWithWitness(fullWitness)
	c.Assert(err, qt.IsNil, qt.Commentf("prove aggregator circuit"))
}
