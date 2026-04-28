package state

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/types"
)

// TransitionArtifact stores the durable metadata for a state transition.
// It is keyed by the resulting root so the confirmed branch can be promoted or
// reconstructed later without replaying the queue entry.
type TransitionArtifact struct {
	ProcessID       types.ProcessID      `json:"processId"`
	RootHashBefore  *big.Int             `json:"rootHashBefore"`
	RootHashAfter   *big.Int             `json:"rootHashAfter"`
	TxHash          *common.Hash         `json:"txHash,omitempty"`
	BlobVersionHash common.Hash          `json:"blobVersionHash"`
	BlobSidecar     *types.BlobTxSidecar `json:"blobSidecar,omitempty"`
	BatchID         []byte               `json:"batchId,omitempty"`
}

// ApplyTransitionArtifact applies the blobs in the artifact to the committed
// state database and promotes the local root to the confirmed one.
func ApplyTransitionArtifact(database db.Database, artifact *TransitionArtifact) error {
	if artifact == nil {
		return fmt.Errorf("nil state transition artifact")
	}
	if artifact.ProcessID == (types.ProcessID{}) {
		return fmt.Errorf("state transition artifact has no processID")
	}
	if artifact.RootHashBefore == nil {
		return fmt.Errorf("state transition artifact has no rootHashBefore")
	}
	if artifact.RootHashAfter == nil {
		return fmt.Errorf("state transition artifact has no rootHashAfter")
	}
	if artifact.BlobSidecar == nil {
		return fmt.Errorf("state transition artifact has no blob sidecar")
	}

	st, err := New(database, artifact.ProcessID)
	if err != nil {
		return fmt.Errorf("failed to open state for process %s: %w", artifact.ProcessID.String(), err)
	}

	if err := st.ApplyBlobSidecarFromRoot(artifact.RootHashBefore, artifact.RootHashAfter, artifact.BlobSidecar); err != nil {
		return fmt.Errorf("failed to restore state from transition artifact: %w", err)
	}

	return nil
}
