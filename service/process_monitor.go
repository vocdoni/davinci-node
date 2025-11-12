package service

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/census3-bigquery/censusdb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

// ProcessMonitor is a service that monitors new voting processes or process
// updates and update them in the local storage.
type ProcessMonitor struct {
	contracts ContractsService
	storage   *storage.Storage
	interval  time.Duration
	mu        sync.Mutex
	cancel    context.CancelFunc
}

// ContractsService defines the interface for web3 contract operations.
type ContractsService interface {
	MonitorProcessCreation(ctx context.Context, interval time.Duration) (<-chan *types.Process, error)
	MonitorProcessStatusChanges(ctx context.Context, interval time.Duration) (<-chan *types.ProcessWithStatusChange, error)
	MonitorProcessStateRootChange(ctx context.Context, interval time.Duration) (<-chan *types.ProcessWithStateRootChange, error)
	CreateProcess(process *types.Process) (*types.ProcessID, *common.Hash, error)
	AccountAddress() common.Address
	WaitTxByHash(hash common.Hash, timeout time.Duration, cb ...func(error)) error
	WaitTxByID(id []byte, timeout time.Duration, cb ...func(error)) error
}

// NewProcessMonitor creates a new ProcessMonitor service. If storage is nil, it uses a memory storage.
func NewProcessMonitor(contracts ContractsService, stg *storage.Storage, interval time.Duration) *ProcessMonitor {
	if stg == nil {
		kv := memdb.New()
		stg = storage.New(kv)
	}
	return &ProcessMonitor{
		contracts: contracts,
		storage:   stg,
		interval:  interval,
	}
}

// Start begins monitoring for new processes. It returns an error if the service
// is already running or if it fails to start monitoring.
func (pm *ProcessMonitor) Start(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.cancel != nil {
		return fmt.Errorf("service already running")
	}

	ctx, cancel := context.WithCancel(ctx)
	pm.cancel = cancel

	newProcChan, err := pm.contracts.MonitorProcessCreation(ctx, pm.interval)
	if err != nil {
		pm.cancel = nil
		return fmt.Errorf("failed to start monitor of process creation: %w", err)
	}

	changedStatusProcChan, err := pm.contracts.MonitorProcessStatusChanges(ctx, pm.interval)
	if err != nil {
		pm.cancel = nil
		return fmt.Errorf("failed to start monitor of process status changes: %w", err)
	}

	stateTransitionChan, err := pm.contracts.MonitorProcessStateRootChange(ctx, pm.interval)
	if err != nil {
		pm.cancel = nil
		return fmt.Errorf("failed to start monitor of process state root changes: %w", err)
	}

	go pm.monitorProcesses(ctx, newProcChan, changedStatusProcChan, stateTransitionChan)
	return nil
}

// Stop halts the monitoring service.
func (pm *ProcessMonitor) Stop() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.cancel != nil {
		pm.cancel()
		pm.cancel = nil
	}
}

func (pm *ProcessMonitor) monitorProcesses(
	ctx context.Context,
	newProcChan <-chan *types.Process,
	changedStatusProcChan <-chan *types.ProcessWithStatusChange,
	stateTransitionChan <-chan *types.ProcessWithStateRootChange,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case process := <-newProcChan:
			// try to update the process if it already exists
			if _, err := pm.storage.Process(new(types.ProcessID).SetBytes(process.ID)); err == nil {
				continue
			}
			// download the census if needed
			if err := pm.ImportCensus(ctx, process); err != nil {
				log.Warnw("failed to download census for new process",
					"pid", process.ID.String(),
					"censusOrigin", process.Census.CensusOrigin.String(),
					"censusURI", process.Census.CensusURI,
					"err", err.Error())
			}
			// if it does not exist, create a new one
			log.Debugw("new process found", "pid", process.ID.String())
			if err := pm.storage.NewProcess(process); err != nil {
				log.Warnw("failed to store new process",
					"pid", process.ID.String(),
					"err", err.Error())
			}
			// initialize the state for the process
			log.Debugw("process state created", "pid", process.ID.String())
		case process := <-changedStatusProcChan:
			log.Debugw("process changed status",
				"pid", process.ID.String(),
				"old", process.OldStatus.String(),
				"new", process.NewStatus.String())
			if err := pm.storage.UpdateProcess(
				new(types.ProcessID).SetBytes(process.ID),
				storage.ProcessUpdateCallbackSetStatus(process.Status),
			); err != nil {
				log.Warnw("failed to update process status",
					"pid", process.ID.String(),
					"err", err.Error())
			}
			if process.Status == types.ProcessStatusResults {
				if err := pm.storage.CleanProcessStaleVotes(process.ID); err != nil {
					log.Warnw("failed to clean stale votes after process finalization",
						"pid", process.ID.String(), "err", err.Error())
				}
			}
		case process := <-stateTransitionChan:
			log.Debugw("process state root changed",
				"pid", process.ID.String(),
				"stateRoot", process.NewStateRoot.String(),
				"voteCount", process.NewVoteCount.String(),
				"voteOverwrittenCount", process.NewVoteOverwrittenCount.String())
			if err := pm.storage.UpdateProcess(
				new(types.ProcessID).SetBytes(process.ID),
				storage.ProcessUpdateCallbackSetStateRoot(
					process.NewStateRoot,
					process.NewVoteCount,
					process.NewVoteOverwrittenCount,
				),
			); err != nil {
				log.Warnw("failed to update process state root",
					"pid", process.ID.String(),
					"err", err.Error())
			}
		}
	}
}

// ImportCensus downloads and imports the census from the given URI based on
// its origin:
//   - For CensusOriginMerkleTree, it expects a URL pointing to a JSON dump of
//     the census merkle tree, downloads it, and imports it into the census DB
//     by its census root.
//
// It returns an error if the download or import fails.
//
// TODO: Think about if this function should be here or in another package
// based on the final implementation of census downloading and importing by
// census origin.
func (pm *ProcessMonitor) ImportCensus(ctx context.Context, process *types.Process) error {
	origin := process.Census.CensusOrigin
	uri := process.Census.CensusURI

	log.Debugw("downloading census",
		"pid", process.ID.String(),
		"origin", process.Census.CensusOrigin.String(),
		"uri", uri)

	var ref *censusdb.CensusRef
	switch process.Census.CensusOrigin {
	case types.CensusOriginMerkleTreeOffchainStaticV1:
		// Check if the URI is a valid URL
		if u, err := url.Parse(uri); err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("invalid URL: %s", uri)
		}
		// Download json dump from URI
		dumpRes, err := http.Get(process.Census.CensusURI)
		if err != nil {
			return fmt.Errorf("failed to download census merkle tree dump from %s: %w", process.Census.CensusURI, err)
		}
		defer dumpRes.Body.Close()

		if dumpRes.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to download census merkle tree dump from %s: status code %d", uri, dumpRes.StatusCode)
		}
		// Decode the JSON as census merkle tree dump
		dump, err := io.ReadAll(dumpRes.Body)
		if err != nil {
			return fmt.Errorf("failed to read census merkle tree dump from %s: %w", uri, err)
		}

		// Import the census merkle tree dump into the census DB
		if ref, err = pm.storage.CensusDB().Import(dump); err != nil {
			return fmt.Errorf("failed to import census merkle tree dump from %s: %w", uri, err)
		}
	default:
		return fmt.Errorf("unsupported census origin: %s", origin.String())
	}
	log.Infow("census imported",
		"pid", process.ID.String(),
		"origin", origin.String(),
		"uri", uri,
		"length", ref.Size(),
		"root", hex.EncodeToString(ref.Root()))
	return nil
}
