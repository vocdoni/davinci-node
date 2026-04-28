package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

// StateSync is a service that synchronizes local state by fetching blobs
// from state transition notifications and applying them to the state tree.
type StateSync struct {
	blobFetcherResolver web3.ProcessBlobFetcherResolver
	storage             *storage.Storage
	queue               chan *types.ProcessWithChanges
	applyFn             func(context.Context, *types.ProcessWithChanges) error
	workers             sync.Map
	cancel              context.CancelFunc
}

type stateSyncWorker struct {
	queue   chan *types.ProcessWithChanges
	applyFn func(context.Context, *types.ProcessWithChanges) error
}

// NewStateSync creates a new StateSync service.
func NewStateSync(
	resolver web3.ProcessBlobFetcherResolver,
	stg *storage.Storage,
) *StateSync {
	ss := &StateSync{
		blobFetcherResolver: resolver,
		storage:             stg,
		queue:               make(chan *types.ProcessWithChanges, 100),
	}
	ss.applyFn = ss.fetchBlobAndApply
	return ss
}

// Start begins the state synchronization service.
func (ss *StateSync) Start(ctx context.Context) error {
	if ss.cancel != nil {
		return fmt.Errorf("StateSync service already running")
	}

	ctx, cancel := context.WithCancel(ctx)
	ss.cancel = cancel
	ss.workers.Clear()

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
			if err := ss.enqueueInWorker(ctx, process); err != nil {
				log.Warnw("statesync enqueue failed",
					"processID", process.ProcessID.String(),
					"error", err)
			}
		}
	}
}

// fetchBlobAndApply fetches an onchain blob and applies it over local state.
func (ss *StateSync) fetchBlobAndApply(ctx context.Context, process *types.ProcessWithChanges) error {
	// First check if NEW state root is already present (i.e. we're synced already), skip sync
	if err := state.RootExists(ss.storage.StateDB(),
		process.ProcessID,
		process.NewStateRoot.MathBigInt()); err == nil {
		return ss.promoteConfirmedStateRoot(process)
	}

	log.Debugw("syncing state from blob",
		"processID", process.ProcessID.String(),
		"txHash", process.TxHash.String(),
		"oldStateRoot", process.OldStateRoot.String(),
		"newStateRoot", process.NewStateRoot.String())

	blobFetcher, err := resolveBlobFetcherForProcess(ss.blobFetcherResolver, process.ProcessID)
	if err != nil {
		return err
	}

	// Fetch blob data using the transaction hash
	blobs, err := blobFetcher.BlobsByTxHash(ctx, *process.TxHash)
	if err != nil {
		return fmt.Errorf("failed to fetch blobs for tx %s: %w", process.TxHash.String(), err)
	}

	if len(blobs) == 0 {
		return fmt.Errorf("no blobs found for tx %s", process.TxHash.String())
	}

	// Apply blob data to reconstruct the state.
	sidecar := &types.BlobTxSidecar{
		Blobs: make([]*types.Blob, 0, len(blobs)),
	}
	for _, blobSidecar := range blobs {
		if blobSidecar == nil || blobSidecar.Blob == nil {
			continue
		}
		sidecar.Blobs = append(sidecar.Blobs, blobSidecar.Blob)
	}
	if len(sidecar.Blobs) == 0 {
		return fmt.Errorf("no usable blobs found for tx %s", process.TxHash.String())
	}

	st, err := state.New(ss.storage.StateDB(), process.ProcessID)
	if err != nil {
		return fmt.Errorf("failed to open state for process %s: %w", process.ProcessID.String(), err)
	}
	if err := st.ApplyBlobSidecarFromRoot(
		process.OldStateRoot.MathBigInt(),
		process.NewStateRoot.MathBigInt(),
		sidecar,
	); err != nil {
		return fmt.Errorf("failed to apply blob data to state: %w", err)
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
		"oldStateRoot", process.OldStateRoot.String(),
		"verifiedStateRoot", newRoot.String())

	return nil
}

func (ss *StateSync) promoteConfirmedStateRoot(process *types.ProcessWithChanges) error {
	st, err := state.New(ss.storage.StateDB(), process.ProcessID)
	if err != nil {
		return fmt.Errorf("failed to open state for process %s: %w", process.ProcessID.String(), err)
	}
	if err := st.SetRootAsBigInt(process.NewStateRoot.MathBigInt()); err != nil {
		return fmt.Errorf("failed to promote confirmed root %s: %w",
			process.NewStateRoot.String(), err)
	}
	log.Debugw("confirmed state root already present locally, promoted root pointer",
		"processID", process.ProcessID.String(),
		"oldStateRoot", process.OldStateRoot.String(),
		"newStateRoot", process.NewStateRoot.String())
	return nil
}

func (ss *StateSync) enqueueInWorker(ctx context.Context, process *types.ProcessWithChanges) error {
	return ss.getOrCreateWorker(ctx, process.ProcessID).enqueue(process)
}

func (ss *StateSync) getOrCreateWorker(ctx context.Context, processID types.ProcessID) *stateSyncWorker {
	v, loaded := ss.workers.LoadOrStore(processID, newStateSyncWorker(ss.applyFn))
	ssw := v.(*stateSyncWorker)
	if !loaded {
		go ssw.run(ctx)
	}
	return ssw
}

func newStateSyncWorker(applyFn func(context.Context, *types.ProcessWithChanges) error) *stateSyncWorker {
	return &stateSyncWorker{
		queue:   make(chan *types.ProcessWithChanges, 100),
		applyFn: applyFn,
	}
}

func resolveBlobFetcherForProcess(resolver web3.ProcessBlobFetcherResolver, processID types.ProcessID) (web3.BlobFetcher, error) {
	if resolver == nil {
		return nil, fmt.Errorf("blob fetcher resolver is not configured")
	}

	blobFetcher, err := resolver.BlobFetcherForProcess(processID)
	if err != nil {
		return nil, fmt.Errorf("resolve blob fetcher for process %s: %w", processID.String(), err)
	}
	if blobFetcher == nil {
		return nil, fmt.Errorf("resolve blob fetcher for process %s: nil blob fetcher", processID.String())
	}
	return blobFetcher, nil
}

func (ssw *stateSyncWorker) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case process := <-ssw.queue:
			if err := ssw.applyFn(ctx, process); err != nil {
				log.Warnw("statesync failed",
					"error", err,
					"processID", process.ProcessID.String(),
					"txHash", process.TxHash.String(),
					"oldStateRoot", process.OldStateRoot.String(),
					"newStateRoot", process.NewStateRoot.String())
			}
		}
	}
}

func (ssw *stateSyncWorker) enqueue(process *types.ProcessWithChanges) error {
	select {
	case ssw.queue <- process:
		return nil
	default:
		return fmt.Errorf("statesync queue for process %s is full", process.ProcessID.String())
	}
}
