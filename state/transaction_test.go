package state_test

import (
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db"
	internaltest "github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/state"
	statetest "github.com/vocdoni/davinci-node/state/testutil"
	"github.com/vocdoni/davinci-node/types"
)

func TestPrepareVotesBatchStagesUntilCommit(t *testing.T) {
	c := qt.New(t)
	backing, st, processID, publicKey := testStateForTx(t)

	rootBefore, err := st.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	vote := statetest.NewVoteForTest(publicKey, 0, 7)
	batch, err := st.PrepareVotesBatch([]*state.Vote{vote})
	c.Assert(err, qt.IsNil)

	stagedRoot, err := batch.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(stagedRoot.Cmp(rootBefore) != 0, qt.IsTrue)

	committed, err := state.New(backing, processID)
	c.Assert(err, qt.IsNil)
	committedRoot, err := committed.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(committedRoot.Cmp(rootBefore) == 0, qt.IsTrue)

	batch.Discard()
	rolledBackRoot, err := st.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(rolledBackRoot.Cmp(rootBefore) == 0, qt.IsTrue)

	vote = statetest.NewVoteForTest(publicKey, 0, 7)
	batch, err = st.PrepareVotesBatch([]*state.Vote{vote})
	c.Assert(err, qt.IsNil)
	stagedRoot, err = batch.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(batch.Commit(), qt.IsNil)

	committed, err = state.New(backing, processID)
	c.Assert(err, qt.IsNil)
	committedRoot, err = committed.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(committedRoot.Cmp(stagedRoot) == 0, qt.IsTrue)
}

func TestPrepareVotesBatchRollsBackOnBlobError(t *testing.T) {
	c := qt.New(t)
	_, st, _, publicKey := testStateForTx(t)

	rootBefore, err := st.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	vote := statetest.NewVoteForTest(publicKey, 0, 7)
	vote.Weight = new(big.Int).Lsh(big.NewInt(1), uint(state.BlobTxBytesPerFieldElement*8))

	_, err = st.PrepareVotesBatch([]*state.Vote{vote})
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "blob eval data")

	rootAfter, err := st.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(rootAfter.Cmp(rootBefore) == 0, qt.IsTrue)
}

func TestLoadSnapshotOnRootDoesNotMoveCurrentRoot(t *testing.T) {
	c := qt.New(t)
	backing, st, processID, publicKey := testStateForTx(t)

	vote := statetest.NewVoteForTest(publicKey, 0, 7)
	c.Assert(st.AddVotesBatch([]*state.Vote{vote}), qt.IsNil)
	rootBefore, err := st.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	vote = statetest.NewVoteForTest(publicKey, 1, 9)
	c.Assert(st.AddVotesBatch([]*state.Vote{vote}), qt.IsNil)
	currentRoot, err := st.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(currentRoot.Cmp(rootBefore) != 0, qt.IsTrue)

	snapshot, err := state.LoadSnapshotOnRoot(backing, processID, rootBefore)
	c.Assert(err, qt.IsNil)
	snapshotRoot, err := snapshot.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(snapshotRoot.Cmp(rootBefore) == 0, qt.IsTrue)

	committed, err := state.New(backing, processID)
	c.Assert(err, qt.IsNil)
	committedRoot, err := committed.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(committedRoot.Cmp(currentRoot) == 0, qt.IsTrue)
}

func TestSpeculativeTransitionsApplySequentiallyFromConfirmedRoots(t *testing.T) {
	c := qt.New(t)
	backing, committed, processID, publicKey := testStateForTx(t)

	initialRoot, err := committed.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	firstVotes := []*state.Vote{
		statetest.NewVoteForTest(publicKey, 0, 7),
	}
	firstRoot, firstSidecar := testStageSpeculativeTransition(t, committed, initialRoot, firstVotes)
	c.Assert(firstRoot.Cmp(initialRoot) != 0, qt.IsTrue)

	c.Assert(committed.ApplyBlobSidecarFromRoot(initialRoot, firstRoot, firstSidecar), qt.IsNil)
	currentRoot, err := committed.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(currentRoot.Cmp(firstRoot), qt.Equals, 0)

	secondVotes := []*state.Vote{
		statetest.NewVoteForTest(publicKey, 1, 11),
	}
	secondRoot, secondSidecar := testStageSpeculativeTransition(t, committed, firstRoot, secondVotes)
	c.Assert(secondRoot.Cmp(firstRoot) != 0, qt.IsTrue)

	c.Assert(committed.ApplyBlobSidecarFromRoot(firstRoot, secondRoot, secondSidecar), qt.IsNil)
	currentRoot, err = committed.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(currentRoot.Cmp(secondRoot), qt.Equals, 0)

	initialSnapshot, err := state.LoadSnapshotOnRoot(backing, processID, initialRoot)
	c.Assert(err, qt.IsNil)
	c.Assert(initialSnapshot.ContainsVoteID(firstVotes[0].VoteID), qt.IsFalse)
	c.Assert(initialSnapshot.ContainsVoteID(secondVotes[0].VoteID), qt.IsFalse)

	firstSnapshot, err := state.LoadSnapshotOnRoot(backing, processID, firstRoot)
	c.Assert(err, qt.IsNil)
	c.Assert(firstSnapshot.ContainsVoteID(firstVotes[0].VoteID), qt.IsTrue)
	c.Assert(firstSnapshot.ContainsVoteID(secondVotes[0].VoteID), qt.IsFalse)

	secondSnapshot, err := state.LoadSnapshotOnRoot(backing, processID, secondRoot)
	c.Assert(err, qt.IsNil)
	c.Assert(secondSnapshot.ContainsVoteID(firstVotes[0].VoteID), qt.IsTrue)
	c.Assert(secondSnapshot.ContainsVoteID(secondVotes[0].VoteID), qt.IsTrue)
}

func TestApplyBlobSidecarFromRootRollsBackRootCheckoutAndWrites(t *testing.T) {
	c := qt.New(t)
	_, st, _, publicKey := testStateForTx(t)

	rootBefore, err := st.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	vote := statetest.NewVoteForTest(publicKey, 0, 7)
	batch, err := st.PrepareVotesBatch([]*state.Vote{vote})
	c.Assert(err, qt.IsNil)
	rootAfter, err := batch.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	blob := batch.BlobEvalData().TxSidecar().Blobs[0]
	batch.Discard()

	err = st.ApplyBlobSidecarFromRoot(rootBefore, rootAfter, &types.BlobTxSidecar{
		Blobs: []*types.Blob{blob, blob},
	})
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "already exists")

	currentRoot, err := st.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(currentRoot.Cmp(rootBefore) == 0, qt.IsTrue)
	c.Assert(st.ContainsVoteID(vote.VoteID), qt.IsFalse)
}

func testStageSpeculativeTransition(
	t *testing.T,
	committed *state.State,
	rootBefore *big.Int,
	votes []*state.Vote,
) (*big.Int, *types.BlobTxSidecar) {
	t.Helper()
	c := qt.New(t)

	speculativeRoot, err := committed.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(speculativeRoot.Cmp(rootBefore), qt.Equals, 0)

	batch, err := committed.PrepareVotesBatch(votes)
	c.Assert(err, qt.IsNil)
	defer batch.Discard()

	rootAfter, err := batch.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(batch.Commit(), qt.IsNil)

	return rootAfter, batch.BlobEvalData().TxSidecar()
}

func testStateForTx(t *testing.T) (db.Database, *state.State, types.ProcessID, ecc.Point) {
	t.Helper()
	c := qt.New(t)

	backing := memdb.New()
	processID := internaltest.RandomProcessID()
	publicKey, _, err := elgamal.GenerateKey(state.Curve)
	c.Assert(err, qt.IsNil)

	st, err := state.New(backing, processID)
	c.Assert(err, qt.IsNil)

	err = st.Initialize(
		types.CensusOriginMerkleTreeOffchainStaticV1.BigInt().MathBigInt(),
		internaltest.BallotModePacked(),
		types.EncryptionKeyFromPoint(publicKey),
	)
	c.Assert(err, qt.IsNil)

	return backing, st, processID, publicKey
}
