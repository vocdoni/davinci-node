package service

import (
	"context"
	"fmt"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

// StateSync is a service that synchronizes local state by fetching blobs
// from state transition notifications and applying them to the state tree.
type StateSync struct {
	contracts ContractsService
	storage   *storage.Storage
	queue     chan *types.ProcessWithChanges
	cancel    context.CancelFunc
}

// NewStateSync creates a new StateSync service.
func NewStateSync(
	contracts ContractsService,
	stg *storage.Storage,
) *StateSync {
	return &StateSync{
		contracts: contracts,
		storage:   stg,
		queue:     make(chan *types.ProcessWithChanges, 100),
	}
}

// Start begins the state synchronization service.
func (ss *StateSync) Start(ctx context.Context) error {
	if ss.cancel != nil {
		return fmt.Errorf("StateSync service already running")
	}

	ctx, cancel := context.WithCancel(ctx)
	ss.cancel = cancel

	go ss.consumeQueue(ctx)
	log.Infow("StateSync service started")
	return nil
}

// Stop halts the state synchronization service.
func (ss *StateSync) Stop() {
	if ss.cancel != nil {
		ss.cancel()
		ss.cancel = nil
		log.Infow("StateSync service stopped")
	}
}

// Notify triggers a state sync of the process. Returns immediately.
func (ss *StateSync) Notify(process *types.ProcessWithChanges) {
	select {
	case ss.queue <- process:
		log.Debugw("state transition notification sent to statesync", "processID", process.ProcessID.String())
	default:
		log.Warnw("statesync notification dropped - channel full", "processID", process.ProcessID.String())
	}
}

// consumeQueue listens for state transition notifications and dispatches goroutines for each of them.
func (ss *StateSync) consumeQueue(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case process := <-ss.queue:
			// Handle each sync request in a separate goroutine to avoid blocking
			go func() {
				if err := ss.fetchBlobAndApply(ctx, process); err != nil {
					log.Warnw("failed to sync state from blob",
						"processID", process.ProcessID.String(),
						"txHash", process.TxHash.String(),
						"error", err.Error())
				}
			}()
		}
	}
}

// fetchBlobAndApply fetches an onchain blob and applies it over local state.
func (ss *StateSync) fetchBlobAndApply(ctx context.Context, process *types.ProcessWithChanges) error {
	// First check if NEW state root is already present (i.e. we're synced already), skip sync
	if _, err := state.LoadOnRoot(ss.storage.StateDB(),
		process.ProcessID,
		process.NewStateRoot.MathBigInt()); err == nil {
		log.Debugf("process %s with state %d is up-to-date, no need for StateSync",
			process.ProcessID.String(), process.NewStateRoot.MathBigInt())
		return nil
	}

	log.Debugw("syncing state from blob",
		"processID", process.ProcessID.String(),
		"txHash", process.TxHash.String(),
		"oldStateRoot", process.OldStateRoot.String(),
		"newStateRoot", process.NewStateRoot.String())

	// Load state at the OLD state root (before the transition)
	st, err := state.LoadOnRoot(ss.storage.StateDB(),
		process.ProcessID,
		process.OldStateRoot.MathBigInt())
	if err != nil {
		return fmt.Errorf("failed to load state on old root %s: %w",
			process.OldStateRoot.String(), err)
	}

	// Fetch blob data using the transaction hash
	blobs, err := ss.contracts.BlobsByTxHash(ctx, *process.TxHash)
	if err != nil {
		return fmt.Errorf("failed to fetch blobs for tx %s: %w", process.TxHash.String(), err)
	}

	if len(blobs) == 0 {
		return fmt.Errorf("no blobs found for tx %s", process.TxHash.String())
	}

	// Apply blob data to reconstruct the state
	for _, blobSidecar := range blobs {
		if err := st.ApplyBlobToState(blobSidecar.Blob); err != nil {
			return fmt.Errorf("failed to apply blob data to state: %w", err)
		}
	}

	// Verify that the reconstructed state matches the expected new state root
	newRoot, err := st.RootAsBigInt()
	if err != nil {
		return fmt.Errorf("failed to calculate new state root: %w", err)
	}

	if newRoot.Cmp(process.NewStateRoot.MathBigInt()) != 0 {
		return fmt.Errorf("state root mismatch: expected %s, got %s",
			process.NewStateRoot.String(), newRoot.String())
	}

	log.Debugw("successfully synced state from blob",
		"processID", process.ProcessID.String(),
		"txHash", process.TxHash.String(),
		"verifiedStateRoot", newRoot.String())

	return nil
}
