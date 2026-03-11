package aggregatortest

import (
	"os"
	"testing"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	qt "github.com/frankban/quicktest"
	circuitstest "github.com/vocdoni/davinci-node/circuits/test"
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

	aggCCS, aggPK, aggVK, err := circuitstest.LoadAggregatorRuntimeArtifacts()
	c.Assert(err, qt.IsNil, qt.Commentf("load aggregator runtime artifacts"))

	fullWitness, err := frontend.NewWitness(assignment, params.AggregatorCurve.ScalarField())
	c.Assert(err, qt.IsNil, qt.Commentf("witness creation"))

	// Prove and verify
	var proof groth16.Proof
	log.Infow("proving and verifying aggregator circuit")
	proof, err = circuitstest.ProveAndVerifyWithWitness(
		params.AggregatorCurve,
		aggCCS,
		aggPK,
		aggVK,
		fullWitness,
		[]backend.ProverOption{stdgroth16.GetNativeProverOptions(
			params.StateTransitionCurve.ScalarField(),
			params.AggregatorCurve.ScalarField(),
		)},
		[]backend.VerifierOption{stdgroth16.GetNativeVerifierOptions(
			params.StateTransitionCurve.ScalarField(),
			params.AggregatorCurve.ScalarField(),
		)},
	)
	c.Assert(err, qt.IsNil, qt.Commentf("prove aggregator circuit"))

	err = circuitstest.VerifyProofWithWitness(
		proof,
		aggVK,
		fullWitness,
		stdgroth16.GetNativeVerifierOptions(
			params.StateTransitionCurve.ScalarField(),
			params.AggregatorCurve.ScalarField(),
		),
	)
	c.Assert(err, qt.IsNil, qt.Commentf("verify public proof"))
}
