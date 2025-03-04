package aggregatortest

import (
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	"github.com/consensys/gnark/std/math/emulated"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/aggregator"
	ballottest "github.com/vocdoni/vocdoni-z-sandbox/circuits/test/ballotproof"
	voteverifiertest "github.com/vocdoni/vocdoni-z-sandbox/circuits/test/voteverifier"
	"github.com/vocdoni/vocdoni-z-sandbox/state"
)

// AggregatorTestResults struct includes relevant data after AggregatorCircuit
// inputs generation
type AggregatorTestResults struct {
	Process circuits.Process[*big.Int]
	Votes   []state.Vote
}

// AggregatorInputsForTest returns the AggregatorTestResults, the placeholder
// and the assignments of a AggregatorCircuit for the processId provided
// generating nValidVotes. If something fails it returns an error.
func AggregatorInputsForTest(processId []byte, nValidVotes int) (
	*AggregatorTestResults, *aggregator.AggregatorCircuit, *aggregator.AggregatorCircuit, error,
) {
	now := time.Now()
	log.Println("Aggregator inputs generation starts")
	// generate users accounts and census
	vvData := []voteverifiertest.VoterTestData{}
	for range nValidVotes {
		privKey, pubKey, address, err := ballottest.GenECDSAaccountForTest()
		if err != nil {
			return nil, nil, nil, err
		}
		vvData = append(vvData, voteverifiertest.VoterTestData{
			PrivKey: privKey,
			PubKey:  pubKey,
			Address: address,
		})
	}
	// generate vote verifier circuit and inputs
	vvInputs, vvPlaceholder, vvAssigments, err := voteverifiertest.VoteVerifierInputsForTest(vvData, processId)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("voteverifier inputs: %w", err)
	}
	// compile vote verifier circuit
	vvCCS, err := frontend.Compile(circuits.VoteVerifierCurve.ScalarField(), r1cs.NewBuilder, &vvPlaceholder)
	if err != nil {
		return nil, nil, nil, err
	}
	vvPk, vvVk, err := groth16.Setup(vvCCS)
	if err != nil {
		return nil, nil, nil, err
	}
	// generate voters proofs
	proofs := [circuits.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{}
	proofsInputsHashes := [circuits.VotesPerBatch]emulated.Element[sw_bn254.ScalarField]{}
	for i := range vvAssigments {
		// parse the witness to the circuit
		fullWitness, err := frontend.NewWitness(&vvAssigments[i], circuits.VoteVerifierCurve.ScalarField())
		if err != nil {
			return nil, nil, nil, err
		}
		// generate the proof
		proof, err := groth16.Prove(vvCCS, vvPk, fullWitness, stdgroth16.GetNativeProverOptions(
			circuits.AggregatorCurve.ScalarField(),
			circuits.VoteVerifierCurve.ScalarField()))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("err proving voteverifier circuit %d: %w", i, err)
		}
		// convert the proof to the circuit proof type
		proofs[i], err = stdgroth16.ValueOfProof[sw_bls12377.G1Affine, sw_bls12377.G2Affine](proof)
		if err != nil {
			return nil, nil, nil, err
		}
		proofsInputsHashes[i] = emulated.ValueOf[sw_bn254.ScalarField](vvInputs.InputsHashes[i])
	}
	// init final assignments stuff
	finalAssigments := &aggregator.AggregatorCircuit{
		ValidProofs:        nValidVotes,
		ProofsInputsHashes: proofsInputsHashes,
		Proofs:             proofs,
	}
	// fill assignments with dummy values
	if err := finalAssigments.FillWithDummy(vvCCS, vvPk, ballottest.TestCircomVerificationKey, nValidVotes); err != nil {
		return nil, nil, nil, err
	}
	// fix the vote verifier verification key
	fixedVk, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bls12377.G1Affine, sw_bls12377.G2Affine, sw_bls12377.GT](vvVk)
	if err != nil {
		return nil, nil, nil, err
	}
	// create final placeholder
	finalPlaceholder := &aggregator.AggregatorCircuit{
		Proofs:          [circuits.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{},
		VerificationKey: fixedVk,
	}
	for i := range circuits.VotesPerBatch {
		finalPlaceholder.Proofs[i] = stdgroth16.PlaceholderProof[sw_bls12377.G1Affine, sw_bls12377.G2Affine](vvCCS)
	}
	// TODO: drop this compat-code when previous circuits are also refactored and can do Votes = vvInputs.Votes
	votes := []state.Vote{}
	for i := range nValidVotes {
		votes = append(votes, state.Vote{
			Address:    vvInputs.Addresses[i].Bytes(),
			Commitment: vvInputs.Commitments[i],
			Nullifier:  vvInputs.Nullifiers[i].Bytes(),
			Ballot:     &vvInputs.Ballots[i],
		})
	}
	log.Printf("Aggregator inputs generation ends, it tooks %s", time.Since(now))
	return &AggregatorTestResults{
		Process: circuits.Process[*big.Int]{
			ID:            vvInputs.ProcessID,
			CensusRoot:    vvInputs.CensusRoot,
			EncryptionKey: vvInputs.EncryptionPubKey,
		},
		Votes: votes,
	}, finalPlaceholder, finalAssigments, nil
}
