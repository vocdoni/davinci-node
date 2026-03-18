package aggregatortest

import (
	"math/big"
	"testing"
	"time"

	"github.com/vocdoni/davinci-node/spec/params"

	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	"github.com/consensys/gnark/std/math/emulated"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	circuitstest "github.com/vocdoni/davinci-node/circuits/test"
	ballottest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	voteverifiertest "github.com/vocdoni/davinci-node/circuits/test/voteverifier"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

// AggregatorInputsForTest returns the AggregatorTestResults, the placeholder
// and the assignment of an AggregatorCircuit for the processID provided
// generating nValidVotes. Uses quicktest assertions instead of returning errors.
func AggregatorInputsForTest(
	t *testing.T,
	processID types.ProcessID,
	censusOrigin types.CensusOrigin,
	nValidVoters int,
) (
	*circuitstest.AggregatorTestResults, *aggregator.AggregatorCircuit, *aggregator.AggregatorCircuit,
) {
	c := qt.New(t)

	startTime := time.Now()
	log.Infow("aggregator inputs generation starts")

	vvData := []voteverifiertest.VoterTestData{}
	for i := range nValidVoters {
		s, err := ballottest.GenDeterministicECDSAaccountForTest(i)
		c.Assert(err, qt.IsNil, qt.Commentf("generate deterministic ECDSA account %d", i))

		vvData = append(vvData, voteverifiertest.VoterTestData{
			PrivKey: s,
			PubKey:  s.PublicKey,
			Address: s.Address(),
		})
	}
	vvInputs, vvPlaceholder, vvAssignments := voteverifiertest.VoteVerifierInputsForTest(t, vvData, processID, censusOrigin)
	voteverifierRuntime, err := voteverifier.Artifacts.LoadOrSetupForCircuit(t.Context(), &vvPlaceholder)
	c.Assert(err, qt.IsNil, qt.Commentf("resolve vote verifier runtime artifacts"))

	// generate voters proofs
	proofs := [params.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{}
	proofsInputsHashes := [params.VotesPerBatch]emulated.Element[sw_bn254.ScalarField]{}
	for i := range vvAssignments {
		proof, err := voteverifierRuntime.ProveAndVerify(&vvAssignments[i])
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
	inputsHash, err := aggInputs.InputsHash()
	c.Assert(err, qt.IsNil, qt.Commentf("calculate inputs hash"))

	// Build the final aggregator assignment.
	assignment := &aggregator.AggregatorCircuit{
		ValidProofs:  nValidVoters,
		BatchHash:    emulated.ValueOf[sw_bn254.ScalarField](inputsHash),
		BallotHashes: proofsInputsHashes,
		Proofs:       proofs,
	}

	// Fill the remaining slots with dummy values.
	dummyAssignment, err := voteverifier.DummyAssignment()
	c.Assert(err, qt.IsNil, qt.Commentf("dummy assignment"))

	dummyProof, err := voteverifierRuntime.ProveAndVerify(dummyAssignment)
	c.Assert(err, qt.IsNil, qt.Commentf("dummy proof"))

	err = assignment.FillWithDummy(nValidVoters, dummyProof)
	c.Assert(err, qt.IsNil, qt.Commentf("fill with dummy values"))

	// fix the vote verifier verification key
	fixedVk, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bls12377.G1Affine, sw_bls12377.G2Affine, sw_bls12377.GT](voteverifierRuntime.VerifyingKey())
	c.Assert(err, qt.IsNil, qt.Commentf("fix vote verifier verification key"))

	// create final placeholder
	finalPlaceholder := &aggregator.AggregatorCircuit{
		Proofs:          [params.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{},
		VerificationKey: fixedVk,
	}
	for i := range params.VotesPerBatch {
		finalPlaceholder.Proofs[i] = stdgroth16.PlaceholderProof[sw_bls12377.G1Affine, sw_bls12377.G2Affine](voteverifierRuntime.ConstraintSystem())
	}
	votes := []*state.Vote{}
	for i := range nValidVoters {
		votes = append(votes, &state.Vote{
			Address:     vvInputs.Addresses[i],
			BallotIndex: types.CalculateBallotIndex(uint64(i)),
			VoteID:      vvInputs.VoteIDs[i],
			Weight:      vvInputs.Weights[i],
			Ballot:      vvInputs.Ballots[i].FromTEtoRTE(),
		})
	}
	log.DebugTime("aggregator inputs generation", startTime)
	return &circuitstest.AggregatorTestResults{
		InputsHash: inputsHash,
		Process: circuits.Process[*big.Int]{
			ID:            vvInputs.ProcessID,
			EncryptionKey: vvInputs.EncryptionPubKey,
			CensusOrigin:  new(big.Int).SetInt64(int64(censusOrigin)),
		},
		Votes: votes,
	}, finalPlaceholder, assignment
}
