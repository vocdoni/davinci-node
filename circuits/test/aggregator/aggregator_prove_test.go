package aggregatortest

import (
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	qt "github.com/frankban/quicktest"
	circuitstest "github.com/vocdoni/davinci-node/circuits/test"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
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

	// Cache for the local compiled circuit artifacts
	cache, err := circuitstest.NewCircuitCache()
	c.Assert(err, qt.IsNil, qt.Commentf("create circuit cache"))
	aggCacheData := circuitstest.AggregatorCacheData{}
	cacheKey := cache.GenerateCacheKey("aggregator", processID, 3)

	// Try to load everything from cache first to avoid regenerating inputs/compile/setup
	cacheErr := cache.LoadData(cacheKey, &aggCacheData, true)
	cacheReady := cacheErr == nil &&
		aggCacheData.ConstraintSystem != nil &&
		aggCacheData.VerifyingKey != nil &&
		aggCacheData.Witness != nil

	if !cacheReady {
		if cacheErr != nil {
			t.Logf("no cache for aggregator circuit (load err: %v), will compile and setup", cacheErr)
		} else {
			t.Logf("aggregator cache incomplete, will compile and setup")
		}

		start := time.Now()
		_, placeholder, assignments := AggregatorInputsForTest(t, processID, types.CensusOriginMerkleTreeOffchainStaticV1, 3)
		t.Logf("inputs generation took %s", time.Since(start))

		t.Logf("compiling aggregator circuit...")
		ccs, err := frontend.Compile(params.AggregatorCurve.ScalarField(), r1cs.NewBuilder, placeholder)
		c.Assert(err, qt.IsNil, qt.Commentf("compile aggregator circuit"))
		aggCacheData.ConstraintSystem = ccs

		t.Logf("setting up aggregator circuit...")
		pk, vk, err := prover.Setup(ccs)
		c.Assert(err, qt.IsNil, qt.Commentf("setup aggregator"))
		aggCacheData.ProvingKey = pk
		aggCacheData.VerifyingKey = vk

		t.Logf("creating witness for aggregator circuit...")
		w, err := frontend.NewWitness(assignments, params.AggregatorCurve.ScalarField())
		c.Assert(err, qt.IsNil, qt.Commentf("witness creation"))
		aggCacheData.Witness = w

		// Save to cache
		err = cache.SaveData(cacheKey, &aggCacheData)
		c.Assert(err, qt.IsNil, qt.Commentf("save aggregator cache"))
	} else {
		t.Logf("using cached aggregator circuit data for key %s", cacheKey)
	}

	// Prove and verify
	var proof groth16.Proof
	t.Logf("proving and verifying aggregator circuit...")
	start := time.Now()
	opts := stdgroth16.GetNativeProverOptions(
		params.StateTransitionCurve.ScalarField(),
		params.AggregatorCurve.ScalarField(),
	)
	proof, err = prover.ProveWithWitness(
		params.AggregatorCurve,
		aggCacheData.ConstraintSystem,
		aggCacheData.ProvingKey,
		aggCacheData.Witness,
		opts,
	)
	c.Assert(err, qt.IsNil, qt.Commentf("prove aggregator circuit"))
	t.Logf("proving took %s", time.Since(start))

	start = time.Now()
	pub, err := aggCacheData.Witness.Public()
	c.Assert(err, qt.IsNil, qt.Commentf("public witness"))
	err = groth16.Verify(proof, aggCacheData.VerifyingKey, pub, stdgroth16.GetNativeVerifierOptions(
		params.StateTransitionCurve.ScalarField(),
		params.AggregatorCurve.ScalarField()))
	c.Assert(err, qt.IsNil, qt.Commentf("verify proof"))
	t.Logf("verification took %s", time.Since(start))
}
