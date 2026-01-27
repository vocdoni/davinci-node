package aggregatortest

import (
	"log"
	"math/big"
	"testing"
	"time"

	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/types/params"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	"github.com/consensys/gnark/std/math/emulated"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	circuitstest "github.com/vocdoni/davinci-node/circuits/test"
	ballottest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	voteverifiertest "github.com/vocdoni/davinci-node/circuits/test/voteverifier"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

// AggregatorInputsForTest returns the AggregatorTestResults, the placeholder
// and the assignments of a AggregatorCircuit for the processID provided
// generating nValidVotes. Uses quicktest assertions instead of returning errors.
func AggregatorInputsForTest(
	t *testing.T,
	processID types.ProcessID,
	censusOrigin types.CensusOrigin,
	nValidVotes int,
) (
	*circuitstest.AggregatorTestResults, *aggregator.AggregatorCircuit, *aggregator.AggregatorCircuit,
) {
	c := qt.New(t)

	now := time.Now()
	log.Println("aggregator inputs generation starts")
	// Use unified cache system for vote verifier data
	cache, err := circuitstest.NewCircuitCache()
	c.Assert(err, qt.IsNil, qt.Commentf("create circuit cache"))

	cacheKey := cache.GenerateCacheKey("voteverifier", processID, nValidVotes)
	cachedData := &circuitstest.VoteVerifierCacheData{}

	var vvPk groth16.ProvingKey
	var vvVk groth16.VerifyingKey
	var vvCCS constraint.ConstraintSystem
	var vvInputs circuitstest.VoteVerifierTestResults
	var vvWitness []witness.Witness

	if err := cache.LoadData(cacheKey, cachedData, true); err != nil {
		// Cache miss - compile and setup vote verifier circuit
		c.Logf("Cache miss for key %s, generating vote verifier circuit data", cacheKey)

		// generate deterministic users accounts and census for consistent caching
		vvData := []voteverifiertest.VoterTestData{}
		for i := range nValidVotes {
			s, err := ballottest.GenDeterministicECDSAaccountForTest(i)
			c.Assert(err, qt.IsNil, qt.Commentf("generate deterministic ECDSA account %d", i))

			vvData = append(vvData, voteverifiertest.VoterTestData{
				PrivKey: s,
				PubKey:  s.PublicKey,
				Address: s.Address(),
			})
		}
		// generate vote verifier circuit and inputs with deterministic ProcessID
		var vvPlaceholder voteverifier.VerifyVoteCircuit
		var vvAssignments []voteverifier.VerifyVoteCircuit
		vvInputs, vvPlaceholder, vvAssignments = voteverifiertest.VoteVerifierInputsForTest(t, vvData, processID, censusOrigin)

		vvCCS, err = frontend.Compile(params.VoteVerifierCurve.ScalarField(), r1cs.NewBuilder, &vvPlaceholder)
		c.Assert(err, qt.IsNil, qt.Commentf("compile vote verifier circuit"))

		pk, vk, err := prover.Setup(vvCCS)
		c.Assert(err, qt.IsNil, qt.Commentf("setup vote verifier circuit"))
		vvPk = pk
		vvVk = vk

		// generate witnesses for each voter
		for i := range vvAssignments {
			// parse the witness to the circuit
			fullWitness, err := frontend.NewWitness(&vvAssignments[i], params.VoteVerifierCurve.ScalarField())
			c.Assert(err, qt.IsNil, qt.Commentf("generate witness for vote verifier circuit %d", i))
			vvWitness = append(vvWitness, fullWitness)
		}

		// Save to cache for future use including CCS
		cachedData.ProvingKey = vvPk
		cachedData.VerifyingKey = vvVk
		cachedData.ConstraintSystem = vvCCS
		cachedData.Inputs = vvInputs
		cachedData.Witness = vvWitness
		err = cache.SaveData(cacheKey, cachedData)
		c.Assert(err, qt.IsNil, qt.Commentf("saving vote verifier data to cache"))
	} else {
		// Cache hit - use cached data
		c.Logf("Cache hit for key %s, using cached vote verifier circuit data", cacheKey)
		vvPk = cachedData.ProvingKey
		vvVk = cachedData.VerifyingKey
		vvCCS = cachedData.ConstraintSystem
		vvWitness = cachedData.Witness
		vvInputs = cachedData.Inputs
	}

	// generate voters proofs
	proofs := [params.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{}
	proofsInputsHashes := [params.VotesPerBatch]emulated.Element[sw_bn254.ScalarField]{}
	for i := range vvWitness {
		// generate the proof (automatically uses GPU if enabled)
		proof, err := prover.ProveWithWitness(params.VoteVerifierCurve, vvCCS, vvPk, vvWitness[i],
			stdgroth16.GetNativeProverOptions(params.AggregatorCurve.ScalarField(),
				params.VoteVerifierCurve.ScalarField()))
		c.Assert(err, qt.IsNil, qt.Commentf("proving voteverifier circuit %d", i))

		// convert the proof to the circuit proof type
		proofs[i], err = stdgroth16.ValueOfProof[sw_bls12377.G1Affine, sw_bls12377.G2Affine](proof)
		c.Assert(err, qt.IsNil, qt.Commentf("convert proof for voter %d", i))
		proofsInputsHashes[i] = emulated.ValueOf[sw_bn254.ScalarField](vvInputs.InputsHashes[i])
	}
	// calculate inputs hash
	aggInputs := aggregator.AggregatorInputs{
		ProofsInputsHashInputs: vvInputs.InputsHashes,
	}
	batchHash, err := aggInputs.BatchHash()
	c.Assert(err, qt.IsNil, qt.Commentf("calculate batchHash"))

	// init final assignments stuff
	finalAssignments := &aggregator.AggregatorCircuit{
		ValidProofs:  nValidVotes,
		BatchHash:    emulated.ValueOf[sw_bn254.ScalarField](batchHash),
		BallotHashes: proofsInputsHashes,
		Proofs:       proofs,
	}
	// fill assignments with dummy values
	err = finalAssignments.FillWithDummy(vvCCS, vvPk, ballotproof.CircomVerificationKey, nValidVotes, nil)
	c.Assert(err, qt.IsNil, qt.Commentf("fill with dummy values"))

	// fix the vote verifier verification key
	fixedVk, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bls12377.G1Affine, sw_bls12377.G2Affine, sw_bls12377.GT](vvVk)
	c.Assert(err, qt.IsNil, qt.Commentf("fix vote verifier verification key"))

	// create final placeholder
	finalPlaceholder := &aggregator.AggregatorCircuit{
		Proofs:          [params.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{},
		VerificationKey: fixedVk,
	}
	for i := range params.VotesPerBatch {
		finalPlaceholder.Proofs[i] = stdgroth16.PlaceholderProof[sw_bls12377.G1Affine, sw_bls12377.G2Affine](vvCCS)
	}
	votes := []state.Vote{}
	for i := range nValidVotes {
		votes = append(votes, state.Vote{
			Address: vvInputs.Addresses[i],
			VoteID:  vvInputs.VoteIDs[i],
			Weight:  vvInputs.Weights[i],
			Ballot:  vvInputs.Ballots[i].FromTEtoRTE(),
		})
	}
	log.Printf("aggregator inputs generation ends, it tooks %s", time.Since(now))
	return &circuitstest.AggregatorTestResults{
		InputsHash: batchHash,
		Process: circuits.Process[*big.Int]{
			ID:            vvInputs.ProcessID,
			EncryptionKey: vvInputs.EncryptionPubKey,
			CensusOrigin:  new(big.Int).SetInt64(int64(censusOrigin)),
		},
		Votes: votes,
	}, finalPlaceholder, finalAssignments
}
