package statetransitiontest

import (
	"math/big"
	"os"
	"testing"

	"github.com/vocdoni/davinci-node/prover"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	circuitstest "github.com/vocdoni/davinci-node/circuits/test"
	aggregatortest "github.com/vocdoni/davinci-node/circuits/test/aggregator"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/state"
	testcensus "github.com/vocdoni/davinci-node/tests/census"
	"github.com/vocdoni/davinci-node/types"
)

// StateTransitionTestResults struct includes relevant data after StateTransitionCircuit
// inputs generation
type StateTransitionTestResults struct {
	Process      circuits.Process[*big.Int]
	Votes        []state.Vote
	PublicInputs *statetransition.PublicInputs
}

// StateTransitionInputsForTest returns the StateTransitionTestResults, the placeholder
// and the assignments of a StateTransitionCircuit for the processId provided
// generating nValidVoters. Uses quicktest assertions instead of returning errors.
func StateTransitionInputsForTest(
	t *testing.T,
	processId *types.ProcessID,
	censusOrigin types.CensusOrigin,
	nValidVoters int,
) (
	*StateTransitionTestResults, *statetransition.StateTransitionCircuit, *statetransition.StateTransitionCircuit,
) {
	c := qt.New(t)

	// Use unified cache system for aggregator data
	cache, err := circuitstest.NewCircuitCache()
	c.Assert(err, qt.IsNil, qt.Commentf("create circuit cache"))

	cacheKey := cache.GenerateCacheKey("aggregator", processId, nValidVoters)
	cachedData := &circuitstest.AggregatorCacheData{}

	var proof groth16.Proof
	var agVk groth16.VerifyingKey
	var fullWitness witness.Witness
	var aggInputs *circuitstest.AggregatorTestResults

	// Try to use cached aggregation proof and vk if available, otherwise generate from scratch
	if err := cache.LoadData(cacheKey, cachedData); err != nil {
		// Cache miss - generate everything from scratch
		c.Logf("Cache miss for key %s, generating aggregator circuit data", cacheKey)

		// generate aggregator circuit and inputs
		var agPlaceholder, aggWitness *aggregator.AggregatorCircuit
		aggInputs, agPlaceholder, aggWitness = aggregatortest.AggregatorInputsForTest(t, processId, censusOrigin, nValidVoters)

		// parse the witness to the circuit
		fullWitness, err = frontend.NewWitness(aggWitness, circuits.AggregatorCurve.ScalarField())
		c.Assert(err, qt.IsNil, qt.Commentf("aggregator witness"))

		// compile aggregator circuit
		agCCS, err := frontend.Compile(circuits.AggregatorCurve.ScalarField(), r1cs.NewBuilder, agPlaceholder)
		c.Assert(err, qt.IsNil, qt.Commentf("aggregator compile"))

		agPk, vk, err := prover.Setup(agCCS)
		c.Assert(err, qt.IsNil, qt.Commentf("aggregator setup"))
		agVk = vk

		// generate the proof (automatically uses GPU if enabled)
		proof, err = prover.ProveWithWitness(circuits.AggregatorCurve, agCCS, agPk, fullWitness,
			stdgroth16.GetNativeProverOptions(circuits.StateTransitionCurve.ScalarField(),
				circuits.AggregatorCurve.ScalarField()))
		c.Assert(err, qt.IsNil, qt.Commentf("proving aggregator circuit"))

		// Save proof, verification key, CCS, and witness to cache for future use
		cachedData.Proof = proof
		cachedData.VerifyingKey = agVk
		cachedData.ConstraintSystem = agCCS
		cachedData.Witness = fullWitness
		cachedData.Inputs = *aggInputs
		err = cache.SaveData(cacheKey, cachedData)
		c.Assert(err, qt.IsNil, qt.Commentf("saving aggregator data to cache"))
	} else {
		// Cache hit - use cached data
		c.Logf("Cache hit for key %s, using cached aggregator circuit data", cacheKey)
		proof = cachedData.Proof
		agVk = cachedData.VerifyingKey
		fullWitness = cachedData.Witness
		aggInputs = &cachedData.Inputs
	}

	// convert the proof to the circuit proof type
	proofInBW6761, err := stdgroth16.ValueOfProof[sw_bw6761.G1Affine, sw_bw6761.G2Affine](proof)
	c.Assert(err, qt.IsNil, qt.Commentf("convert aggregator proof"))

	// convert the public inputs to the circuit public inputs type
	publicWitness, err := fullWitness.Public()
	c.Assert(err, qt.IsNil, qt.Commentf("convert aggregator public inputs"))

	err = groth16.Verify(proof, agVk, publicWitness, stdgroth16.GetNativeVerifierOptions(
		circuits.StateTransitionCurve.ScalarField(),
		circuits.AggregatorCurve.ScalarField()))
	c.Assert(err, qt.IsNil, qt.Commentf("aggregator verify"))

	// reencrypt the votes with deterministic K for consistent caching
	reencryptionK := circuitstest.GenerateDeterministicK(processId, nValidVoters)

	// get the encryption key from the aggregator inputs
	encryptionKey := state.Curve.New().SetPoint(aggInputs.Process.EncryptionKey.PubKey[0], aggInputs.Process.EncryptionKey.PubKey[1])
	// init final assignments stuff
	s := newState(c, aggInputs.Process.ID, circuits.MockBallotMode(), censusOrigin, aggInputs.Process.EncryptionKey)

	err = s.StartBatch()
	c.Assert(err, qt.IsNil, qt.Commentf("start batch"))

	// iterate over the votes, reencrypting each time the zero ballot with the
	// correct k value and adding it to the state
	lastK := new(big.Int).Set(reencryptionK)
	for _, v := range aggInputs.Votes {
		v.ReencryptedBallot, lastK, err = v.Ballot.Reencrypt(encryptionKey, lastK)
		c.Assert(err, qt.IsNil, qt.Commentf("failed to reencrypt ballot"))

		err = s.AddVote(&v)
		c.Assert(err, qt.IsNil, qt.Commentf("add vote"))
	}

	err = s.EndBatch()
	c.Assert(err, qt.IsNil, qt.Commentf("end batch"))

	// add census data to witness
	censusRoot, censusProofs, err := testcensus.CensusProofsForCircuitTest(
		aggInputs.Votes,
		types.CensusOrigin(aggInputs.Process.CensusOrigin.Uint64()),
		new(types.ProcessID).SetBytes(aggInputs.Process.ID.Bytes()),
	)
	c.Assert(err, qt.IsNil, qt.Commentf("generate census proofs for test"))

	witness, publicInputs, err := statetransition.GenerateWitness(
		s,
		new(types.BigInt).SetBigInt(censusRoot),
		censusProofs,
		new(types.BigInt).SetBigInt(reencryptionK),
	)
	c.Assert(err, qt.IsNil, qt.Commentf("generate witness"))

	witness.AggregatorProof = proofInBW6761

	// create final placeholder
	circuitPlaceholder := CircuitPlaceholder()
	// fix the vote verifier verification key
	fixedVk, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl](agVk)
	c.Assert(err, qt.IsNil, qt.Commentf("aggregator vk"))

	circuitPlaceholder.AggregatorVK = fixedVk
	return &StateTransitionTestResults{
		Process:      aggInputs.Process,
		Votes:        aggInputs.Votes,
		PublicInputs: publicInputs,
	}, circuitPlaceholder, witness
}

func newState(c *qt.C,
	processId *big.Int,
	ballotMode circuits.BallotMode[*big.Int],
	censusOrigin types.CensusOrigin,
	encryptionKey circuits.EncryptionKey[*big.Int],
) *state.State {
	dir, err := os.MkdirTemp(os.TempDir(), "statetransition")
	c.Assert(err, qt.IsNil, qt.Commentf("create temp dir"))

	db, err := metadb.New("pebble", dir)
	c.Assert(err, qt.IsNil, qt.Commentf("create metadb"))

	s, err := state.New(db, processId)
	c.Assert(err, qt.IsNil, qt.Commentf("create state"))

	err = s.Initialize(
		censusOrigin.BigInt().MathBigInt(),
		ballotMode,
		encryptionKey,
	)
	c.Assert(err, qt.IsNil, qt.Commentf("initialize state"))

	return s
}
