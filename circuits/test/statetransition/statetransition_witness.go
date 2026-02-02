package statetransitiontest

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	"github.com/consensys/gnark/std/recursion/groth16"

	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	statetest "github.com/vocdoni/davinci-node/state/testutil"

	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

func CircuitPlaceholder() *statetransition.StateTransitionCircuit {
	proof, vk := DummyAggProofPlaceholder()
	return CircuitPlaceholderWithProof(proof, vk)
}

func CircuitPlaceholderWithProof(
	proof *groth16.Proof[sw_bw6761.G1Affine, sw_bw6761.G2Affine],
	vk *groth16.VerifyingKey[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl],
) *statetransition.StateTransitionCircuit {
	return &statetransition.StateTransitionCircuit{
		AggregatorProof: *proof,
		AggregatorVK:    *vk,
	}
}

func NewTransitionWithVotes(t *testing.T, s *state.State, votes ...state.Vote) *statetransition.StateTransitionCircuit {
	reencryptionK, err := elgamal.RandK()
	if err != nil {
		t.Fatal(err)
	}
	originalEncKey := s.EncryptionKey()
	encryptionKey := state.Curve.New().SetPoint(originalEncKey.PubKey[0], originalEncKey.PubKey[1])
	if err := s.StartBatch(); err != nil {
		t.Fatal(err)
	}
	lastK := new(big.Int).Set(reencryptionK)
	for _, v := range votes {
		v.ReencryptedBallot, lastK, err = v.Ballot.Reencrypt(encryptionKey, lastK)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.AddVote(&v); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.EndBatch(); err != nil {
		t.Fatal(err)
	}

	censusOrigin := types.CensusOrigin(s.CensusOrigin().Uint64())
	processID, err := types.BigIntToProcessID(s.ProcessID())
	if err != nil {
		t.Fatal(err)
	}

	censusRoot, censusProofs, err := CensusProofsForCircuitTest(votes, censusOrigin, processID)
	if err != nil {
		t.Fatal(err)
	}

	witness, _, err := statetransition.GenerateWitness(
		s,
		new(types.BigInt).SetBigInt(censusRoot),
		censusProofs,
		new(types.BigInt).SetBigInt(reencryptionK))
	if err != nil {
		t.Fatal(err)
	}

	// Calculate the actual aggregator hash from the vote data.
	// This matches how the circuit recalculates the hash during verification.
	// Get the ballot mode from state and convert to types.BallotMode
	circuitBallotMode := s.BallotMode()
	ballotMode := &types.BallotMode{
		NumFields:      uint8(circuitBallotMode.NumFields.Int64()),
		UniqueValues:   circuitBallotMode.UniqueValues.Cmp(big.NewInt(1)) == 0,
		MaxValue:       (*types.BigInt)(circuitBallotMode.MaxValue),
		MinValue:       (*types.BigInt)(circuitBallotMode.MinValue),
		MaxValueSum:    (*types.BigInt)(circuitBallotMode.MaxValueSum),
		MinValueSum:    (*types.BigInt)(circuitBallotMode.MinValueSum),
		CostExponent:   uint8(circuitBallotMode.CostExponent.Int64()),
		CostFromWeight: circuitBallotMode.CostFromWeight.Cmp(big.NewInt(1)) == 0,
	}

	hashes := make([]*big.Int, 0, len(votes))
	for _, v := range votes {
		h, err := ballotproof.BallotInputsHashGnark(
			processID,
			ballotMode,
			encryptionKey,
			v.Address.Bytes(),
			v.VoteID.BigInt(),
			v.Ballot,
			types.BigIntConverter(v.Weight),
		)
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, h.MathBigInt())
	}
	aggInputs := aggregator.AggregatorInputs{
		ProofsInputsHashInputs: hashes,
	}
	inputsHash, err := aggInputs.InputsHash()
	if err != nil {
		t.Fatal(err)
	}

	proof, vk, err := DummyAggProof(len(votes), inputsHash)
	if err != nil {
		t.Fatal(err)
	}
	witness.AggregatorProof = *proof
	witness.AggregatorVK = *vk
	return witness
}

// NewTransitionWithOverwrittenVotes returns a witness that includes an overwritten vote
func NewTransitionWithOverwrittenVotes(t *testing.T, origin types.CensusOrigin) *statetransition.StateTransitionCircuit {
	// First initialize a state with a transition of 2 new votes,
	s := statetest.NewRandomState(t, origin)
	publicKey := statetest.EncryptionKeyAsECCPoint(s)
	_ = NewTransitionWithVotes(t, s,
		*statetest.NewVoteForTest(publicKey, 0, 10),
		*statetest.NewVoteForTest(publicKey, 1, 10),
	)
	// so now we can return a transition where vote 1 is overwritten
	// and add 3 more votes
	return NewTransitionWithVotes(t, s,
		*statetest.NewVoteForTest(publicKey, 1, 20),
		*statetest.NewVoteForTest(publicKey, 2, 20),
		*statetest.NewVoteForTest(publicKey, 3, 20),
		*statetest.NewVoteForTest(publicKey, 4, 20),
	)
}
