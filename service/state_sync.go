package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

// StateSync is a service that synchronizes local state by fetching blobs
// from state transition notifications and applying them to the state tree.
type StateSync struct {
	contracts     ContractsService
	storage       *storage.Storage
	notifications chan *types.ProcessWithStateRootChange
	mu            sync.Mutex
	cancel        context.CancelFunc
}

// NewStateSync creates a new StateSync service.
func NewStateSync(
	contracts ContractsService,
	stg *storage.Storage,
) *StateSync {
	return &StateSync{
		contracts:     contracts,
		storage:       stg,
		notifications: make(chan *types.ProcessWithStateRootChange, 100),
	}
}

// Start begins the state synchronization service.
func (ss *StateSync) Start(ctx context.Context) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.cancel != nil {
		return fmt.Errorf("StateSync service already running")
	}

	ctx, cancel := context.WithCancel(ctx)
	ss.cancel = cancel

	go ss.processSyncRequests(ctx)
	log.Infow("StateSync service started")
	return nil
}

// Stop halts the state synchronization service.
func (ss *StateSync) Stop() {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.cancel != nil {
		ss.cancel()
		ss.cancel = nil
		log.Infow("StateSync service stopped")
	}
}

// processSyncRequests listens for state transition notifications and processes them.
func (ss *StateSync) processSyncRequests(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case process := <-ss.notifications:
			// Handle each sync request in a separate goroutine to avoid blocking
			go func(proc *types.ProcessWithStateRootChange) {
				if err := ss.syncStateFromBlob(ctx, proc); err != nil {
					log.Warnw("failed to sync state from blob",
						"pid", proc.ID.String(),
						"txHash", proc.TxHash.String(),
						"err", err.Error())
				}
			}(process)
		}
	}
}

// syncStateFromBlob fetches an onchain blob and applies it over local state.
func (ss *StateSync) syncStateFromBlob(ctx context.Context, process *types.ProcessWithStateRootChange) error {
	log.Debugw("syncing state from blob",
		"pid", process.ID.String(),
		"txHash", process.TxHash.String(),
		"newStateRoot", process.NewStateRoot.String())

	// Fetch blob data using the transaction hash
	blobs, err := ss.contracts.BlobsByTxHash(ctx, process.TxHash)
	if err != nil {
		return fmt.Errorf("failed to fetch blobs for tx %s: %w", process.TxHash.String(), err)
	}

	if len(blobs) == 0 {
		return fmt.Errorf("no blobs found for tx %s", process.TxHash.String())
	}

	// Get the current process to find the old state root
	processID := new(types.ProcessID).SetBytes(process.ID)
	currentProcess, err := ss.storage.Process(processID)
	if err != nil {
		return fmt.Errorf("failed to get current process: %w", err)
	}

	// Load state at the OLD state root (before the transition)
	st, err := state.LoadOnRoot(ss.storage.StateDB(),
		process.ID.BigInt().MathBigInt(),
		currentProcess.StateRoot.MathBigInt())
	if err != nil {
		return fmt.Errorf("failed to load state on old root %s: %w",
			currentProcess.StateRoot.String(), err)
	}

	// Apply blob data to reconstruct the state
	for _, blob := range blobs {
		parsedBlob, err := state.ParseBlobData(blob.Blob[:])
		if err != nil {
			return fmt.Errorf("failed to parse blob data: %w", err)
		}

		if err := st.ApplyBlobToState(parsedBlob); err != nil {
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
			process.NewStateRoot.String(),
			newRoot.String())
	}

	log.Debugw("successfully synced state from blob",
		"pid", process.ID.String(),
		"txHash", process.TxHash.String(),
		"verifiedStateRoot", newRoot.String())

	return nil
}
