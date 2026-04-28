package state_test

import (
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/state"
	statetest "github.com/vocdoni/davinci-node/state/testutil"
	"github.com/vocdoni/davinci-node/types"
)

func TestApplyTransitionArtifactRestoresState(t *testing.T) {
	c := qt.New(t)
	backing, st, processID, publicKey := testStateForTx(t)

	rootBefore, err := st.RootAsBigInt()
	c.Assert(err, qt.IsNil)

	vote := statetest.NewVoteForTest(publicKey, 0, 7)
	batch, err := st.PrepareVotesBatch([]*state.Vote{vote})
	c.Assert(err, qt.IsNil)
	rootAfter, err := batch.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	blobSidecar := batch.BlobEvalData().TxSidecar()
	c.Assert(batch.Commit(), qt.IsNil)

	c.Assert(state.ApplyTransitionArtifact(backing, &state.TransitionArtifact{
		ProcessID:      processID,
		RootHashBefore: rootBefore,
		RootHashAfter:  rootAfter,
		BlobSidecar:    blobSidecar,
	}), qt.IsNil)

	restored, err := state.LoadSnapshotOnRoot(backing, processID, rootAfter)
	c.Assert(err, qt.IsNil)
	gotRoot, err := restored.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(gotRoot.Cmp(rootAfter), qt.Equals, 0)

	c.Assert(restored.ContainsVoteID(vote.VoteID), qt.IsTrue)
	committed, err := state.New(backing, processID)
	c.Assert(err, qt.IsNil)
	committedRoot, err := committed.RootAsBigInt()
	c.Assert(err, qt.IsNil)
	c.Assert(committedRoot.Cmp(rootAfter), qt.Equals, 0)

	_ = types.BlobTxSidecar{}
}
