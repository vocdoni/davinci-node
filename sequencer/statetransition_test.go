package sequencer

import (
	"testing"

	"github.com/consensys/gnark/backend/groth16"
	qt "github.com/frankban/quicktest"
	statetransitiontest "github.com/vocdoni/davinci-node/circuits/test/statetransition"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	statetest "github.com/vocdoni/davinci-node/state/testutil"
	"github.com/vocdoni/davinci-node/types"
)

func TestProcessStateTransitionBatchRollsBackRootOnAssignmentError(t *testing.T) {
	c := qt.New(t)

	processState := statetest.NewRandomState(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	rootBefore, err := processState.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	publicKey := statetest.EncryptionKeyAsECCPoint(processState)
	votes := statetest.NewVotesForTest(publicKey, 2, 10)

	kSeed, err := specutil.RandomK()
	c.Assert(err, qt.IsNil)

	lastK := new(types.BigInt).SetBigInt(kSeed).MathBigInt()
	for _, vote := range votes {
		vote.ReencryptedBallot, lastK, err = vote.Ballot.Reencrypt(publicKey, lastK)
		c.Assert(err, qt.IsNil)
	}

	processID, err := types.BigIntToProcessID(processState.ProcessID())
	c.Assert(err, qt.IsNil)

	censusRoot, censusProofs, err := statetransitiontest.CensusProofsForCircuitTest(
		t,
		votes,
		types.CensusOriginMerkleTreeOffchainStaticV1,
		processID,
	)
	c.Assert(err, qt.IsNil)

	var innerProof groth16.Proof

	_, _, _, err = new(Sequencer).processStateTransitionBatch(
		processState,
		new(types.BigInt).SetBigInt(censusRoot),
		censusProofs,
		votes,
		new(types.BigInt).SetBigInt(kSeed),
		innerProof,
	)
	c.Assert(err, qt.Not(qt.IsNil))

	rootAfter, err := processState.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(rootAfter.Cmp(rootBefore), qt.Equals, 0)

	containsVote := processState.ContainsVoteID(votes[0].VoteID)
	c.Assert(containsVote, qt.IsFalse)
}

func TestStateBatchToAssignmentLeavesAdvancedRootOnLateError(t *testing.T) {
	c := qt.New(t)

	processState := statetest.NewRandomState(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	rootBefore, err := processState.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	publicKey := statetest.EncryptionKeyAsECCPoint(processState)
	votes := statetest.NewVotesForTest(publicKey, 2, 10)

	kSeed, err := specutil.RandomK()
	c.Assert(err, qt.IsNil)

	lastK := new(types.BigInt).SetBigInt(kSeed).MathBigInt()
	for _, vote := range votes {
		vote.ReencryptedBallot, lastK, err = vote.Ballot.Reencrypt(publicKey, lastK)
		c.Assert(err, qt.IsNil)
	}

	processID, err := types.BigIntToProcessID(processState.ProcessID())
	c.Assert(err, qt.IsNil)

	censusRoot, censusProofs, err := statetransitiontest.CensusProofsForCircuitTest(
		t,
		votes,
		types.CensusOriginMerkleTreeOffchainStaticV1,
		processID,
	)
	c.Assert(err, qt.IsNil)

	var innerProof groth16.Proof

	_, _, err = new(Sequencer).stateBatchToAssignment(
		processState,
		votes,
		new(types.BigInt).SetBigInt(censusRoot),
		censusProofs,
		new(types.BigInt).SetBigInt(kSeed),
		innerProof,
	)
	c.Assert(err, qt.Not(qt.IsNil))

	rootAfter, err := processState.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(rootAfter.Cmp(rootBefore), qt.Not(qt.Equals), 0)

	containsVote := processState.ContainsVoteID(votes[0].VoteID)
	c.Assert(containsVote, qt.IsTrue)
}
