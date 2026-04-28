package storage

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db/prefixeddb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

func TestStateTransitionArtifact_RoundTripByRootAfter(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := testutil.RandomProcessID()
	rootBefore := big.NewInt(1234)
	rootAfter := big.NewInt(5678)
	txHash := common.HexToHash("0x1234")

	artifact := &state.TransitionArtifact{
		ProcessID:       pid,
		RootHashBefore:  rootBefore,
		RootHashAfter:   rootAfter,
		TxHash:          &txHash,
		BlobVersionHash: common.HexToHash("0x9999"),
		BlobSidecar: &types.BlobTxSidecar{
			Version: types.BlobTxSidecarVersion1,
		},
	}

	c.Assert(stg.PushStateTransitionArtifact(artifact), qt.IsNil)

	retrieved, err := stg.GetStateTransitionArtifact(pid, rootAfter)
	c.Assert(err, qt.IsNil)
	c.Assert(retrieved.ProcessID, qt.DeepEquals, pid)
	c.Assert(retrieved.RootHashBefore.Cmp(rootBefore), qt.Equals, 0)
	c.Assert(retrieved.RootHashAfter.Cmp(rootAfter), qt.Equals, 0)
	c.Assert(retrieved.TxHash, qt.Not(qt.IsNil))
	c.Assert(*retrieved.TxHash, qt.DeepEquals, txHash)
	c.Assert(retrieved.BlobVersionHash, qt.DeepEquals, artifact.BlobVersionHash)
	c.Assert(retrieved.BlobSidecar, qt.Not(qt.IsNil))
	c.Assert(retrieved.BlobSidecar.Version, qt.Equals, types.BlobTxSidecarVersion1)
}

func TestStateTransitionArtifact_RemoveByRootAfter(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := testutil.RandomProcessID()
	rootAfter := big.NewInt(5678)

	c.Assert(stg.PushStateTransitionArtifact(&state.TransitionArtifact{
		ProcessID:      pid,
		RootHashAfter:  rootAfter,
		RootHashBefore: big.NewInt(1234),
	}), qt.IsNil)

	c.Assert(stg.removeStateTransitionArtifact(pid, rootAfter), qt.IsNil)

	_, err := stg.GetStateTransitionArtifact(pid, rootAfter)
	c.Assert(err, qt.Equals, ErrNotFound)
}

func TestStateTransitionArtifact_IsNamespacedByProcessID(t *testing.T) {
	c := qt.New(t)
	stg := newTestStorage(t)
	defer stg.Close()

	pid := testutil.RandomProcessID()
	rootAfter := big.NewInt(5678)

	c.Assert(stg.PushStateTransitionArtifact(&state.TransitionArtifact{
		ProcessID:      pid,
		RootHashBefore: big.NewInt(1234),
		RootHashAfter:  rootAfter,
	}), qt.IsNil)

	reader := prefixeddb.NewPrefixedReader(stg.DB(), stateTransitionArtifactPrefix)
	keys := make([][]byte, 0, 1)
	err := reader.Iterate(pid.Bytes(), func(k, _ []byte) bool {
		keyCopy := make([]byte, len(k))
		copy(keyCopy, k)
		keys = append(keys, keyCopy)
		return true
	})
	c.Assert(err, qt.IsNil)
	c.Assert(keys, qt.HasLen, 1)
	c.Assert(keys[0], qt.DeepEquals, rootAfter.Bytes())
}
