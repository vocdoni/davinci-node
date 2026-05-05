package service

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/census"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

// CensusDownloaderConfig holds the configuration for the CensusDownloader. It
// includes:
//   - CleanUpInterval: time duration between cleanup checks for pending
//     censuses.
//   - Expiration: time duration after which a pending census is considered
//     expired (completed or failed).
//   - Cooldown: time duration to wait before retrying a failed census download.
//   - AttemptTimeout: maximum time allowed for a single download/import attempt.
//   - Attempts: maximum number of attempts to download and import a census.
type CensusDownloaderConfig struct {
	CleanUpInterval      time.Duration
	OnchainCheckInterval time.Duration
	Expiration           time.Duration
	Cooldown             time.Duration
	AttemptTimeout       time.Duration
	Attempts             int
	ConcurrentDownloads  int
}

// DefaultCensusDownloaderConfig provides default values for the CensusDownloaderConfig.
var DefaultCensusDownloaderConfig = CensusDownloaderConfig{
	CleanUpInterval:      time.Second * 5,
	OnchainCheckInterval: time.Second * 5,
	Attempts:             5,
	AttemptTimeout:       30 * time.Second,
	Expiration:           time.Minute * 2,
	Cooldown:             time.Second * 5,
	ConcurrentDownloads:  4,
}

// DownloadStatus holds the status of a census download attempt. It is for
// internal use by the CensusDownloader.
type DownloadStatus struct {
	census      *types.Census
	chainID     uint64
	Complete    bool
	Attempts    int
	LastErr     error
	Terminal    bool
	lastUpdated time.Time
}

// internalCensus is a wrapper around types.Census that includes additional
// information needed for processing the census download, update and import it.
type internalCensus struct {
	*types.Census
	ProcessedElements int
	ProcessID         types.ProcessID
	ChainID           uint64
}

// CensusDownloader is responsible for downloading and importing censuses
// asynchronously. It maintains a queue of censuses to download and tracks
// the status of each download attempt.
type CensusDownloader struct {
	queue             chan internalCensus
	config            CensusDownloaderConfig
	ctx               context.Context
	cancel            context.CancelFunc
	contractsResolver web3.ProcessContractsResolver
	storage           *storage.Storage
	importer          *census.CensusImporter
	censusStatus      map[string]DownloadStatus
	onchainCensuses   sync.Map
	mu                sync.RWMutex
	workers           sync.WaitGroup
}

const censusDownloadStatusPollInterval = 100 * time.Millisecond

// NewCensusDownloader creates a new CensusDownloader instance with the given
// process-scoped contracts resolver, storage, and configuration.
func NewCensusDownloader(
	contractsResolver web3.ProcessContractsResolver,
	stg *storage.Storage,
	config CensusDownloaderConfig,
) *CensusDownloader {
	if config.AttemptTimeout <= 0 {
		config.AttemptTimeout = DefaultCensusDownloaderConfig.AttemptTimeout
	}
	return &CensusDownloader{
		queue:             make(chan internalCensus, 100),
		contractsResolver: contractsResolver,
		storage:           stg,
		importer: census.NewCensusImporter(
			stg,
			census.JSONImporter(),
			census.GraphQLImporter(nil),
		),
		censusStatus:    make(map[string]DownloadStatus),
		onchainCensuses: sync.Map{},
		config:          config,
	}
}

// Start begins the CensusDownloader's processing loop. It listens for new
// censuses to download from the DownloadQueue channel and processes them
// asynchronously. If the context is canceled, the downloader stops processing.
func (cd *CensusDownloader) Start(ctx context.Context) error {
	cd.mu.Lock()
	cd.ctx, cd.cancel = context.WithCancel(ctx)
	runCtx := cd.ctx
	cd.mu.Unlock()

	cd.workers.Go(func() {
		// Tickers for periodic tasks: onchain census checks and cleanup of
		// pending censuses
		onchainCheckTicker := time.NewTicker(cd.config.OnchainCheckInterval)
		defer onchainCheckTicker.Stop()
		cleanUpTicker := time.NewTicker(cd.config.CleanUpInterval)
		defer cleanUpTicker.Stop()

		for {
			select {
			case <-runCtx.Done():
				return
			case <-onchainCheckTicker.C:
				// Check for updates to on-chain censuses
				cd.checkOnchainCensuses()
			case <-cleanUpTicker.C:
				// Clean up expired pending censuses
				cd.cleanUpPendingCensuses()
			}
		}
	})

	for range cd.concurrentDownloads() {
		cd.workers.Go(func() {
			for {
				select {
				case <-runCtx.Done():
					return
				case icensus := <-cd.queue:
					if !cd.addPendingCensus(icensus) {
						continue
					}
					if err := cd.processCensusDownload(runCtx, icensus); err != nil {
						log.Warnw("census download failed", "census", icensus.Census, "err", err)
					}
				}
			}
		})
	}
	return nil
}

// Stop stops the CensusDownloader's processing loop by canceling its context.
func (cd *CensusDownloader) Stop() {
	cd.mu.RLock()
	cancel := cd.cancel
	cd.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
	cd.workers.Wait()
}

// DownloadCensus adds the specified census to the download queue for
// asynchronous processing. The processID is required so dynamic on-chain
// censuses can resolve the process runtime, fetch the on-chain root from the
// correct network, and derive the chain-scoped identity used to track that
// census internally. It returns the final census root (after any necessary
// updates) and an error if the operation fails.
func (cd *CensusDownloader) DownloadCensus(processID types.ProcessID, censusInfo *types.Census) (types.HexBytes, error) {
	runCtx, err := cd.downloaderContext()
	if err != nil {
		return nil, fmt.Errorf("census downloader unavailable: %w", err)
	}
	// Create the internal census structure
	icensus := internalCensus{
		Census:            censusInfo,
		ProcessedElements: 0,
		ProcessID:         processID,
	}
	// Add on-chain census to the on-chain census map if applicable
	if icensus.CensusOrigin == types.CensusOriginMerkleTreeOnchainDynamicV1 {
		if icensus, err = cd.addOnchainCensus(icensus); err != nil {
			return nil, fmt.Errorf("failed to add on-chain census: %w", err)
		}
	}
	// Add the census to the queue to be downloaded
	log.Infow("starting census download",
		"origin", icensus.CensusOrigin.String(),
		"root", icensus.CensusRoot.String(),
		"uri", icensus.CensusURI,
		"address", icensus.ContractAddress.String())
	select {
	case <-runCtx.Done():
		return nil, fmt.Errorf("census downloader stopped: %w", runCtx.Err())
	case cd.queue <- icensus:
	}
	return icensus.CensusRoot, nil
}

// DownloadCensusStatus retrieves the current download status of the specified
// census. The processID is required so dynamic on-chain censuses can resolve
// the process runtime and look up the chain-scoped census identity. It returns
// the DownloadStatus and a boolean indicating whether the census is found in
// the pending list.
func (cd *CensusDownloader) DownloadCensusStatus(processID types.ProcessID, census *types.Census) (DownloadStatus, bool) {
	chainID, err := cd.chainIDForCensus(processID, census)
	if err != nil {
		return DownloadStatus{}, false
	}
	return cd.downloadCensusStatus(chainID, census)
}

func (cd *CensusDownloader) downloadCensusStatus(chainID uint64, census *types.Census) (DownloadStatus, bool) {
	cd.mu.RLock()
	defer cd.mu.RUnlock()
	status, exists := cd.censusStatus[censusKey(census, chainID)]
	return status, exists
}

// OnCensusDownloaded registers a callback function that will be called when
// the specified census has been downloaded and imported. The callback will
// be called with an error if the download or import failed, or nil if it
// succeeded. The processID is required so dynamic on-chain censuses can
// resolve the process runtime, identify the correct chain-scoped census entry,
// and check the imported census on the correct network. It allows callers to
// wait asynchronously for a census to be ready, and then execute custom logic
// based on the result.
func (cd *CensusDownloader) OnCensusDownloaded(processID types.ProcessID, census *types.Census, ctx context.Context, callback func(error)) {
	chainID, err := cd.chainIDForCensus(processID, census)
	if err != nil {
		callback(err)
		return
	}

	go func() {
		ticker := time.NewTicker(censusDownloadStatusPollInterval)
		defer ticker.Stop()

		innerCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		for {
			if _, err := cd.storage.LoadCensus(chainID, census); err == nil {
				callback(nil)
				return
			}

			status, exists := cd.downloadCensusStatus(chainID, census)
			if exists {
				switch {
				case status.Terminal && status.LastErr != nil:
					// Return the last error if the downloader has reached a
					// terminal error.
					callback(status.LastErr)
					return
				case status.LastErr != nil && status.Attempts >= cd.attempts():
					// Return the last error if the downloader has reached the
					// maximum number of attempts.
					callback(status.LastErr)
					return
				case status.Complete:
					// If the census download is complete, clean up the pending
					// status and call the callback with nil error.
					cd.CleanUp(status.chainID, status.census)
					callback(nil)
					return
				}
			}

			select {
			case <-innerCtx.Done():
				callback(fmt.Errorf("context done before census downloaded"))
				return
			case <-ticker.C:
			}
		}
	}()
}

// attempts returns the effective number of attempts to use for downloads.
// It normalizes non-positive configuration values to at least 1 attempt.
func (cd *CensusDownloader) attempts() int {
	if cd.config.Attempts <= 0 {
		return 1
	}
	return cd.config.Attempts
}

// concurrentDownloads returns the effective number of concurrent workers used
// to process the census download queue.
func (cd *CensusDownloader) concurrentDownloads() int {
	if cd.config.ConcurrentDownloads <= 0 {
		return 1
	}
	return cd.config.ConcurrentDownloads
}

// processCensusDownload attempts to download and import the given census. It
// retries the download and import process up to the configured number of
// attempts. After each attempt, it updates the status of the census in the
// internal tracking map.
func (cd *CensusDownloader) processCensusDownload(ctx context.Context, census internalCensus) error {
	var importErr error
	for attempt := range cd.attempts() {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("census download canceled: %w", err)
		}
		initialProcessedElements := census.ProcessedElements

		attemptCtx := ctx
		cancel := func() {}
		if cd.config.AttemptTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, cd.config.AttemptTimeout)
		}
		census.ProcessedElements, importErr = cd.importer.ImportCensus(attemptCtx, census.ChainID, census.Census, census.ProcessedElements)
		cancel()
		cd.updateInternalStatus(census, importErr)
		if importErr == nil {
			if census.ProcessedElements > initialProcessedElements {
				log.Infow("census imported successfully",
					"attempt", attempt+1,
					"root", census.CensusRoot.String(),
					"uri", census.CensusURI,
					"newElements", census.ProcessedElements-initialProcessedElements,
					"origin", census.CensusOrigin.String(),
					"address", census.ContractAddress.String())
			}
			return nil
		}
		if isTerminalDownloadError(importErr) {
			log.Warnw("census import failed permanently",
				"error", importErr,
				"attempt", attempt+1,
				"root", census.CensusRoot.String(),
				"uri", census.CensusURI,
				"origin", census.CensusOrigin.String(),
				"address", census.ContractAddress.String())
			return importErr
		}
	}

	log.Warnw("census import failed",
		"error", importErr,
		"attempts", cd.attempts(),
		"root", census.CensusRoot.String(),
		"uri", census.CensusURI,
		"origin", census.CensusOrigin.String())
	return importErr
}

// downloaderContext returns the context used by the downloader. If the
// downloader context is not started, it returns an error.
func (cd *CensusDownloader) downloaderContext() (context.Context, error) {
	cd.mu.RLock()
	defer cd.mu.RUnlock()
	if cd.ctx == nil {
		return nil, fmt.Errorf("not started")
	}
	return cd.ctx, nil
}

// waitTimeout returns the total time budget that the downloader will wait
// for a census to be imported, based on the configured number of attempts,
// per-attempt timeout and cooldown between attempts.
//
// The returned duration is:
//
//	1s (to cover the 1s OnCensusDownloaded polling tick) +
//	Attempts * AttemptTimeout +
//	(Attempts - 1) * Cooldown   (when Attempts > 1).
//
// Note that the per-attempt timeout does not increase between attempts; each
// attempt uses the same AttemptTimeout value.
func (cd *CensusDownloader) waitTimeout() time.Duration {
	timeout := time.Second
	attempts := cd.attempts()
	if cd.config.AttemptTimeout > 0 {
		timeout += time.Duration(attempts) * cd.config.AttemptTimeout
	}
	if cd.config.Cooldown > 0 && attempts > 1 {
		timeout += time.Duration(attempts-1) * cd.config.Cooldown
	}
	return timeout
}

// addPendingCensus adds a census to the internal tracking map of pending
// censuses. It returns false when the census is already tracked.
func (cd *CensusDownloader) addPendingCensus(icensus internalCensus) bool {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	key := censusKey(icensus.Census, icensus.ChainID)
	if _, exists := cd.censusStatus[key]; exists {
		return false
	}
	cd.censusStatus[key] = DownloadStatus{
		Attempts: 0,
		census:   icensus.Census,
		chainID:  icensus.ChainID,
	}
	return true
}

func (cd *CensusDownloader) addOnchainCensus(icensus internalCensus) (internalCensus, error) {
	// Convert the root to a contract address and validate it
	if icensus.ContractAddress == (common.Address{}) {
		return internalCensus{}, fmt.Errorf("invalid on-chain census contract address")
	}
	contracts, err := resolveContractsForCensusProcess(cd.contractsResolver, icensus.ProcessID)
	if err != nil {
		return internalCensus{}, err
	}
	icensus.ChainID = contracts.ChainID
	// Fetch the current census root from the on-chain contract and update
	// it in the original census
	icensus.CensusRoot, err = contracts.FetchOnchainCensusRoot(icensus.ContractAddress)
	if err != nil {
		return internalCensus{}, fmt.Errorf("failed to fetch on-chain census root: %w", err)
	}
	// Store the on-chain census information for later processing
	cd.onchainCensuses.Store(dynamicOnchainCensusKey(icensus.ChainID, icensus.ContractAddress), icensus)
	return icensus, nil
}

// updateInternalStatus updates the status of a pending census download
// attempt.
func (cd *CensusDownloader) updateInternalStatus(icensus internalCensus, err error) {
	// Update also on-chain census map if applicable
	if icensus.CensusOrigin == types.CensusOriginMerkleTreeOnchainDynamicV1 {
		current, ok := cd.loadOnchainCensus(icensus.ChainID, icensus.ContractAddress)
		if ok && current.ProcessedElements < icensus.ProcessedElements {
			cd.onchainCensuses.Swap(dynamicOnchainCensusKey(icensus.ChainID, icensus.ContractAddress), icensus)
		}
	}

	// Lock the mutex to update the census status map
	cd.mu.Lock()
	defer cd.mu.Unlock()

	// Ensure the census exists in the pending map
	key := censusKey(icensus.Census, icensus.ChainID)
	if status, exists := cd.censusStatus[key]; exists {
		// Update the status with the current attempt results
		status.lastUpdated = time.Now()
		status.Complete = err == nil
		status.Terminal = isTerminalDownloadError(err)
		status.Attempts++
		if status.Terminal {
			status.LastErr = fmt.Errorf("terminal census download failure: %w", err)
		} else if status.Attempts < cd.attempts() {
			status.LastErr = err
		} else if err != nil {
			status.LastErr = fmt.Errorf("maximum attempts reached: %w", err)
		}
		cd.censusStatus[key] = status
	}
}

// checkOnchainCensuses periodically re-checks on-chain dynamic Merkle Tree
// censuses for root updates. When the on-chain root changes, the previous
// Complete status is cleaned up so the new root can be re-queued.
func (cd *CensusDownloader) checkOnchainCensuses() {
	runCtx, err := cd.downloaderContext()
	if err != nil {
		return
	}

	cd.onchainCensuses.Range(func(key, value any) bool {
		icensus, ok := value.(internalCensus)
		if !ok {
			return true
		}
		// Skip censuses that are currently being downloaded (in progress)
		if status, exists := cd.DownloadCensusStatus(icensus.ProcessID, icensus.Census); exists && !status.Complete {
			return true
		}

		// Save the old root before re-fetching
		oldRoot := icensus.CensusRoot

		// Re-fetch the on-chain root to check for updates
		icensus, err = cd.addOnchainCensus(icensus)
		if err != nil {
			log.Warnw("failed to check on-chain census", "address", icensus.ContractAddress.Hex(), "error", err)
			return true
		}

		// Only re-queue when the root actually changed
		if icensus.CensusRoot.Equal(oldRoot) {
			return true
		}

		// Clean up the previous Complete status so addPendingCensus accepts
		// the new root under the same chain+contract key.
		cd.CleanUp(icensus.ChainID, icensus.Census)

		select {
		case <-runCtx.Done():
			return false
		case cd.queue <- icensus:
		}
		return true
	})
}

func (cd *CensusDownloader) loadOnchainCensus(chainID uint64, address common.Address) (internalCensus, bool) {
	value, ok := cd.onchainCensuses.Load(dynamicOnchainCensusKey(chainID, address))
	if !ok {
		return internalCensus{}, false
	}
	icensus, ok := value.(internalCensus)
	return icensus, ok
}

// cleanUpPendingCensuses removes expired pending censuses from the internal
// tracking map.
func (cd *CensusDownloader) cleanUpPendingCensuses() {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	now := time.Now()
	for _, status := range cd.censusStatus {
		if status.Terminal {
			continue
		}
		if status.lastUpdated.Add(cd.config.Expiration).Before(now) {
			cd.cleanUpStatusUnsafe(status.chainID, status.census)
		}
	}
}

// isTerminalDownloadError returns true if the given error is a terminal
// download error.
func isTerminalDownloadError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "status code 404") ||
		strings.Contains(errMsg, "non-200 response: 404")
}

// CleanUp is a thread-safe wrapper around cleanUpStatusUnsafe that locks
// the mutex before calling the unsafe version to remove a census status from
// the internal tracking map based on the given census identity.
func (cd *CensusDownloader) CleanUp(chainID uint64, census *types.Census) {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	cd.cleanUpStatusUnsafe(chainID, census)
}

// cleanUpStatusUnsafe removes a census status from the internal tracking
// map based on the given census root. The caller should ensure that holds
// the mutex lock before calling this function to avoid data races.
func (cd *CensusDownloader) cleanUpStatusUnsafe(chainID uint64, census *types.Census) {
	delete(cd.censusStatus, censusKey(census, chainID))
}

// censusKey generates the identity key used to track census downloads. Most
// censuses are identified by their root hash, while on-chain dynamic censuses
// are identified by their chain-scoped contract address so root refreshes do
// not create separate pending entries for the same contract on the same chain.
func censusKey(census *types.Census, chainID uint64) string {
	if census == nil {
		return ""
	}
	if census.CensusOrigin == types.CensusOriginMerkleTreeOnchainDynamicV1 {
		return dynamicOnchainCensusKey(chainID, census.ContractAddress)
	}
	return census.CensusRoot.String()
}

func (cd *CensusDownloader) chainIDForCensus(processID types.ProcessID, census *types.Census) (uint64, error) {
	if census == nil || census.CensusOrigin != types.CensusOriginMerkleTreeOnchainDynamicV1 {
		return 0, nil
	}

	contracts, err := resolveContractsForCensusProcess(cd.contractsResolver, processID)
	if err != nil {
		return 0, err
	}
	return contracts.ChainID, nil
}

func dynamicOnchainCensusKey(chainID uint64, address common.Address) string {
	var chainIDBytes [8]byte
	binary.BigEndian.PutUint64(chainIDBytes[:], chainID)
	return string(append(chainIDBytes[:], address.Bytes()...))
}

func resolveContractsForCensusProcess(resolver web3.ProcessContractsResolver, processID types.ProcessID) (*web3.Contracts, error) {
	if resolver == nil {
		return nil, fmt.Errorf("census contracts resolver is not configured")
	}

	contracts, err := resolver.ContractsForProcess(processID)
	if err != nil {
		return nil, fmt.Errorf("resolve census contracts for process %s: %w", processID.String(), err)
	}
	if contracts == nil {
		return nil, fmt.Errorf("resolve census contracts for process %s: nil contracts", processID.String())
	}
	return contracts, nil
}
