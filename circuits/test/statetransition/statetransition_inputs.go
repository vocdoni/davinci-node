package statetransitiontest

import (
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark/backend"
	groth16bw6761 "github.com/consensys/gnark/backend/groth16/bw6-761"
	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/spec/params"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	circuitstest "github.com/vocdoni/davinci-node/circuits/test"
	aggregatortest "github.com/vocdoni/davinci-node/circuits/test/aggregator"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/state"
	statetest "github.com/vocdoni/davinci-node/state/testutil"
	"github.com/vocdoni/davinci-node/types"
	imt "github.com/vocdoni/lean-imt-go"
	imtcensus "github.com/vocdoni/lean-imt-go/census"
	imtcircuit "github.com/vocdoni/lean-imt-go/circuit"
)

// fixed seed for CSP testing
const testCSPSeed = "1f1e0cd27b4ecd1b71b6333790864ace2870222c"

// StateTransitionTestResults struct includes relevant data after StateTransitionCircuit
// inputs generation
type StateTransitionTestResults struct {
	Process      circuits.Process[*big.Int]
	Votes        []*state.Vote
	PublicInputs *statetransition.PublicInputs
}

// StateTransitionInputsForTest returns the StateTransitionTestResults, the placeholder
// and the assignment of a StateTransitionCircuit for the processID provided
// generating nValidVoters. Uses quicktest assertions instead of returning errors.
func StateTransitionInputsForTest(
	t *testing.T,
	processID types.ProcessID,
	censusOrigin types.CensusOrigin,
	nValidVoters int,
) (
	*StateTransitionTestResults, *statetransition.StateTransitionCircuit, *statetransition.StateTransitionCircuit,
) {
	c := qt.New(t)

	var err error
	var proof groth16.Proof
	var agVk groth16.VerifyingKey
	var fullWitness witness.Witness
	var aggInputs *circuitstest.AggregatorTestResults
	var aggAssignment *aggregator.AggregatorCircuit
	aggInputs, _, aggAssignment = aggregatortest.AggregatorInputsForTest(t, processID, censusOrigin, nValidVoters)

	fullWitness, err = frontend.NewWitness(aggAssignment, params.AggregatorCurve.ScalarField())
	c.Assert(err, qt.IsNil, qt.Commentf("aggregator witness"))

	agCCS, agPK, agVK, err := circuitstest.LoadAggregatorRuntimeArtifacts()
	c.Assert(err, qt.IsNil, qt.Commentf("load aggregator runtime artifacts"))
	agVk = agVK

	proverOpts := stdgroth16.GetNativeProverOptions(
		params.StateTransitionCurve.ScalarField(),
		params.AggregatorCurve.ScalarField(),
	)
	verifierOpts := stdgroth16.GetNativeVerifierOptions(
		params.StateTransitionCurve.ScalarField(),
		params.AggregatorCurve.ScalarField(),
	)
	proof, err = circuitstest.ProveAndVerifyWithWitness(
		params.AggregatorCurve,
		agCCS,
		agPK,
		agVK,
		fullWitness,
		[]backend.ProverOption{proverOpts},
		[]backend.VerifierOption{verifierOpts},
	)
	c.Assert(err, qt.IsNil, qt.Commentf("proving aggregator circuit"))

	if proof == nil {
		c.Logf("aggregator proof is nil")
	} else if proofBW, ok := proof.(*groth16bw6761.Proof); ok {
		c.Logf(
			"aggregator proof curve=%s ar{onCurve=%t inSubGroup=%t infinity=%t} krs{onCurve=%t inSubGroup=%t infinity=%t} bs{onCurve=%t inSubGroup=%t infinity=%t}",
			proofBW.CurveID().String(),
			proofBW.Ar.IsOnCurve(),
			proofBW.Ar.IsInSubGroup(),
			proofBW.Ar.IsInfinity(),
			proofBW.Krs.IsOnCurve(),
			proofBW.Krs.IsInSubGroup(),
			proofBW.Krs.IsInfinity(),
			proofBW.Bs.IsOnCurve(),
			proofBW.Bs.IsInSubGroup(),
			proofBW.Bs.IsInfinity(),
		)
	} else {
		c.Logf("aggregator proof type mismatch: %T", proof)
	}

	if agVk != nil {
		c.Logf("aggregator vk curve=%s", agVk.CurveID().String())
	}

	// convert the proof to the circuit proof type
	proofInBW6761, err := stdgroth16.ValueOfProof[sw_bw6761.G1Affine, sw_bw6761.G2Affine](proof)
	c.Assert(err, qt.IsNil, qt.Commentf("convert aggregator proof"))

	err = circuitstest.VerifyProofWithWitness(
		proof,
		agVk,
		fullWitness,
		stdgroth16.GetNativeVerifierOptions(
			params.StateTransitionCurve.ScalarField(),
			params.AggregatorCurve.ScalarField(),
		),
	)
	c.Assert(err, qt.IsNil, qt.Commentf("aggregator verify"))

	// Reencrypt the votes with deterministic K for reproducible test data.
	reencryptionK := testutil.DeterministicK(processID, nValidVoters)

	// get the encryption key from the aggregator inputs
	encryptionKey := state.Curve.New().SetPoint(aggInputs.Process.EncryptionKey.PubKey[0], aggInputs.Process.EncryptionKey.PubKey[1])
	// Build the final state transition assignment.
	s := statetest.NewStateForTest(t, processID, testutil.BallotModePacked(), censusOrigin, types.EncryptionKeyFromPoint(encryptionKey))

	// iterate over the votes, reencrypting each time the zero ballot with the
	// correct k value
	lastK := new(big.Int).Set(reencryptionK)
	for i := range aggInputs.Votes {
		aggInputs.Votes[i].ReencryptedBallot, lastK, err = aggInputs.Votes[i].Ballot.Reencrypt(encryptionKey, lastK)
		c.Assert(err, qt.IsNil, qt.Commentf("failed to reencrypt ballot"))
	}

	// add census data to witness
	censusRoot, censusProofs, err := CensusProofsForCircuitTest(
		t,
		aggInputs.Votes,
		censusOrigin,
		processID,
	)
	c.Assert(err, qt.IsNil, qt.Commentf("generate census proofs for test"))
	err = s.AddVotesBatch(aggInputs.Votes)
	c.Assert(err, qt.IsNil, qt.Commentf("add votes batch"))

	assignment, publicInputs, err := statetransition.GenerateAssignment(
		s,
		new(types.BigInt).SetBigInt(censusRoot),
		censusProofs,
		new(types.BigInt).SetBigInt(reencryptionK),
	)
	c.Assert(err, qt.IsNil, qt.Commentf("generate assignment"))

	assignment.AggregatorProof = proofInBW6761

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
	}, circuitPlaceholder, assignment
}

// CensusProofsForCircuitTest generates the census proofs required for the
// state transition circuit tests based on the provided votes and census
// origin. It returns the census root, the generated census proofs ready to
// be used in the statetransition circuit, and an error if the process fails.
// It supports both Merkle tree and CSP-based by initializing a CSP instance
// or generating a Merkle tree census as needed.
func CensusProofsForCircuitTest(
	t *testing.T,
	votes []*state.Vote,
	origin types.CensusOrigin,
	pid types.ProcessID,
) (*big.Int, statetransition.CensusProofs, error) {
	t.Helper()
	t.Logf("generating testing census with '%s' origin", origin.String())
	var root *big.Int
	merkleProofs := [params.VotesPerBatch]imtcircuit.MerkleProof{}
	cspProofs := [params.VotesPerBatch]csp.CSPProof{}
	switch {
	case origin.IsMerkleTree():
		// generate the census merkle tree and set the census root
		census, err := CensusIMTForTest(votes)
		if err != nil {
			return nil, statetransition.CensusProofs{}, fmt.Errorf("error generating census merkle tree: %w", err)
		}
		defer func() {
			if err := census.Close(); err != nil {
				t.Logf("warning: failed to close census IMT: %v", err)
			}
		}()
		var ok bool
		if root, ok = census.Root(); !ok {
			return nil, statetransition.CensusProofs{}, fmt.Errorf("error getting census merkle tree root")
		}
		// generate the merkle tree census proofs for each voter and fill the
		// csp proofs with dummy data
		for i := range params.VotesPerBatch {
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
	case origin.IsCSP():
		// instance a csp for testing
		eddsaCSP, err := csp.New(origin, []byte(testCSPSeed))
		if err != nil {
			return nil, statetransition.CensusProofs{}, fmt.Errorf("failed to create csp: %w", err)
		}
		// get the root and generate the csp proofs for each voter
		root = eddsaCSP.CensusRoot().Root.BigInt().MathBigInt()
		for i := range params.VotesPerBatch {
			// add dummy merkle proof
			merkleProofs[i] = statetransition.DummyMerkleProof()
			if i < len(votes) {
				// generate csp proof for the voter address
				addr := common.BytesToAddress(votes[i].Address.Bytes())
				cspProof, err := eddsaCSP.GenerateProof(pid, addr, new(types.BigInt).SetBigInt(votes[i].Weight))
				if err != nil {
					return nil, statetransition.CensusProofs{}, fmt.Errorf("failed to generate census proof: %w", err)
				}
				votes[i].BallotIndex = types.CalculateBallotIndex(cspProof.VoterIndex)
				// convert to gnark csp proof
				gnarkCSPProof, err := csp.CensusProofToCSPProof(types.CensusOriginCSPEdDSABabyJubJubV1.CurveID(), cspProof)
				if err != nil {
					return nil, statetransition.CensusProofs{}, fmt.Errorf("failed to convert census proof to gnark proof: %w", err)
				}
				cspProofs[i] = *gnarkCSPProof
			} else {
				cspProofs[i] = statetransition.DummyCSPProof()
			}
		}
	default:
		return nil, statetransition.CensusProofs{}, fmt.Errorf("unsupported census origin: %s", origin.String())
	}
	return root, statetransition.CensusProofs{
		MerkleProofs: merkleProofs,
		CSPProofs:    cspProofs,
	}, nil
}

// CensusIMTForTest creates a CensusIMT instance for testing purposes including
// the provided votes as census participants. It returns the initialized
// CensusIMT or an error if the process fails.
func CensusIMTForTest(votes []*state.Vote) (*imtcensus.CensusIMT, error) {
	entriesByIndex := map[uint64]struct {
		address common.Address
		weight  *big.Int
	}{}
	var maxIndex uint64
	for _, v := range votes {
		ballotIndex := v.BallotIndex.Uint64()
		if ballotIndex < params.BallotMin {
			return nil, fmt.Errorf("ballot index out of range for census vote: %d", ballotIndex)
		}
		voterIndex := ballotIndex - params.BallotMin
		entriesByIndex[voterIndex] = struct {
			address common.Address
			weight  *big.Int
		}{
			address: common.BigToAddress(v.Address),
			weight:  v.Weight,
		}
		if voterIndex > maxIndex {
			maxIndex = voterIndex
		}
	}

	// Create a unique directory name to avoid lock conflicts
	// Include timestamp and process info for uniqueness
	censusDir := os.TempDir() + fmt.Sprintf("/census_imt_test_%d", time.Now().UnixNano())

	// Initialize the census merkle tree
	censusTree, err := imtcensus.NewCensusIMTWithPebble(censusDir, imt.PoseidonHasher)
	if err != nil {
		return nil, fmt.Errorf("failed to create census IMT: %w", err)
	}

	bAddresses := make([]common.Address, 0, maxIndex+1)
	bWeights := make([]*big.Int, 0, maxIndex+1)
	for i := uint64(0); i <= maxIndex; i++ {
		entry, ok := entriesByIndex[i]
		if !ok {
			// Fill gaps so generated Merkle proof indexes remain aligned with VoterIndex.
			bAddresses = append(bAddresses, testutil.DeterministicAddress(i))
			bWeights = append(bWeights, big.NewInt(testutil.Weight))
			continue
		}
		bAddresses = append(bAddresses, entry.address)
		bWeights = append(bWeights, entry.weight)
	}
	if err := censusTree.AddBulk(bAddresses, bWeights); err != nil {
		return nil, fmt.Errorf("failed to add bulk to census IMT: %w", err)
	}
	return censusTree, nil
}
