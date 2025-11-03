package statetransitiontest

import (
	"fmt"
	"log"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/vocdoni/davinci-node/prover"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	circuitstest "github.com/vocdoni/davinci-node/circuits/test"
	aggregatortest "github.com/vocdoni/davinci-node/circuits/test/aggregator"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
	imt "github.com/vocdoni/lean-imt-go"
	imtcensus "github.com/vocdoni/lean-imt-go/census"
	imtcircuit "github.com/vocdoni/lean-imt-go/circuit"
)

const testCSPSeed = "1f1e0cd27b4ecd1b71b6333790864ace2870222c"

// StateTransitionTestResults struct includes relevant data after StateTransitionCircuit
// inputs generation
type StateTransitionTestResults struct {
	Process circuits.Process[*big.Int]
	Votes   []state.Vote
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
	censusRoot, censusProofs, err := CensusProofsForTest(
		aggInputs.Votes,
		types.CensusOrigin(aggInputs.Process.CensusOrigin.Uint64()),
		new(types.ProcessID).SetBytes(aggInputs.Process.ID.Bytes()),
	)
	c.Assert(err, qt.IsNil, qt.Commentf("generate census proofs for test"))

	witness, err := statetransition.GenerateWitness(
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
		Process: aggInputs.Process,
		Votes:   aggInputs.Votes,
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

func CensusProofsForTest(votes []state.Vote, origin types.CensusOrigin, pid *types.ProcessID) (*big.Int, statetransition.CensusProofs, error) {
	log.Printf("generating testing census with '%s' origin", origin.String())
	var root *big.Int
	merkleProofs := [types.VotesPerBatch]imtcircuit.MerkleProof{}
	cspProofs := [types.VotesPerBatch]csp.CSPProof{}
	switch origin {
	case types.CensusOriginMerkleTree:
		// generate the census with voters information
		votersData := map[*big.Int]*big.Int{}
		for _, v := range votes {
			votersData[v.Address] = v.Weight
		}
		// generate the census merkle tree and set the census root
		census, err := censusMerkleTreeForTest(votersData)
		if err != nil {
			return nil, statetransition.CensusProofs{}, fmt.Errorf("error generating census merkle tree: %w", err)
		}
		var ok bool
		if root, ok = census.Root(); !ok {
			return nil, statetransition.CensusProofs{}, fmt.Errorf("error getting census merkle tree root")
		}
		// generate the merkle tree census proofs for each voter and fill the
		// csp proofs with dummy data
		for i := range types.VotesPerBatch {
			if i < len(votes) {
				addr := common.BigToAddress(votes[i].Address)
				mkproof, err := census.GenerateProof(addr)
				if err != nil {
					return nil, statetransition.CensusProofs{}, fmt.Errorf("error generating census proof for address %s: %w", addr.Hex(), err)
				}
				merkleProofs[i] = imtcircuit.CensusProofToMerkleProof(mkproof)
			} else {
				merkleProofs[i] = statetransition.DummyMerkleProof()
			}
			cspProofs[i] = statetransition.DummyCSPProof()
		}
	default:
		// instance a csp for testing
		eddsaCSP, err := csp.New(origin, []byte(testCSPSeed))
		if err != nil {
			return nil, statetransition.CensusProofs{}, fmt.Errorf("failed to create csp: %w", err)
		}
		// get the root and generate the csp proofs for each voter
		root = eddsaCSP.CensusRoot().Root.BigInt().MathBigInt()
		for i := range types.VotesPerBatch {
			// add dummy merkle proof
			merkleProofs[i] = statetransition.DummyMerkleProof()
			if i < len(votes) {
				// generate csp proof for the voter address
				addr := common.BytesToAddress(votes[i].Address.Bytes())
				cspProof, err := eddsaCSP.GenerateProof(pid, addr)
				if err != nil {
					return nil, statetransition.CensusProofs{}, fmt.Errorf("failed to generate census proof: %w", err)
				}
				// convert to gnark csp proof
				gnarkCSPProof, err := csp.CensusProofToCSPProof(types.CensusOriginCSPEdDSABN254.CurveID(), cspProof)
				if err != nil {
					return nil, statetransition.CensusProofs{}, fmt.Errorf("failed to convert census proof to gnark proof: %w", err)
				}
				cspProofs[i] = *gnarkCSPProof
			} else {
				cspProofs[i] = statetransition.DummyCSPProof()
			}
		}
	}
	return root, statetransition.CensusProofs{
		MerkleProofs: merkleProofs,
		CSPProofs:    cspProofs,
	}, nil
}

func censusMerkleTreeForTest(data map[*big.Int]*big.Int) (*imtcensus.CensusIMT, error) {
	// Create a unique directory name to avoid lock conflicts
	// Include timestamp and process info for uniqueness
	timestamp := time.Now().UnixNano()
	censusDir := fmt.Sprintf("../assets/census_%d", timestamp)

	// Ensure the assets directory exists
	if err := os.MkdirAll("../assets", 0o755); err != nil {
		return nil, fmt.Errorf("failed to create assets directory: %w", err)
	}

	// Initialize the census merkle tree
	censusTree, err := imtcensus.NewCensusIMTWithPebble(censusDir, imt.PoseidonHasher)
	if err != nil {
		return nil, fmt.Errorf("failed to create census IMT: %w", err)
	}

	// Clean up the census directory when done
	defer func() {
		if err := censusTree.Close(); err != nil {
			log.Printf("Warning: failed to close census IMT: %v", err)
		}
		if err := os.RemoveAll(censusDir); err != nil {
			log.Printf("Warning: failed to cleanup census directory %s: %v", censusDir, err)
		}
	}()

	bAddresses, bWeights := []common.Address{}, []*big.Int{}
	for address, weight := range data {
		bAddresses = append(bAddresses, common.BigToAddress(address))
		bWeights = append(bWeights, weight)
	}
	if err := censusTree.AddBulk(bAddresses, bWeights); err != nil {
		return nil, fmt.Errorf("failed to add bulk to census IMT: %w", err)
	}
	return censusTree, nil
}
