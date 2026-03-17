package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/census"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
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
}

// DefaultCensusDownloaderConfig provides default values for the CensusDownloaderConfig.
var DefaultCensusDownloaderConfig = CensusDownloaderConfig{
	CleanUpInterval:      time.Second * 5,
	OnchainCheckInterval: time.Second * 5,
	Attempts:             5,
	AttemptTimeout:       30 * time.Second,
	Expiration:           time.Minute * 2,
	Cooldown:             time.Second * 5,
}

// DownloadStatus holds the status of a census download attempt. It is for
// internal use by the CensusDownloader.
type DownloadStatus struct {
	census      *types.Census
	Complete    bool
	Attempts    int
	LastErr     error
	lastUpdated time.Time
}

// internalCensus is a wrapper around types.Census that includes additional
// information needed for processing the census download, update and import it.
type internalCensus struct {
	*types.Census
	ProcessedElements int
}

// OnchainCensusFetcher defines the interface for fetching on-chain census
// roots. It should be provided to the CensusImporter to handle dynamic
// on-chain Merkle Tree censuses.
type OnchainCensusFetcher interface {
	FetchOnchainCensusRoot(address common.Address) (types.HexBytes, error)
}

// CensusDownloader is responsible for downloading and importing censuses
// asynchronously. It maintains a queue of censuses to download and tracks
// the status of each download attempt.
type CensusDownloader struct {
	queue           chan internalCensus
	config          CensusDownloaderConfig
	ctx             context.Context
	cancel          context.CancelFunc
	onchainFetcher  OnchainCensusFetcher
	storage         *storage.Storage
	importer        *census.CensusImporter
	censusStatus    map[string]DownloadStatus
	onchainCensuses sync.Map
	mu              sync.RWMutex
}

// NewCensusDownloader creates a new CensusDownloader instance with the given
// ContractsService, Storage, and configuration.
func NewCensusDownloader(
	onchainFetcher OnchainCensusFetcher,
	stg *storage.Storage,
	config CensusDownloaderConfig,
) *CensusDownloader {
	if config.AttemptTimeout <= 0 {
		config.AttemptTimeout = DefaultCensusDownloaderConfig.AttemptTimeout
	}
	return &CensusDownloader{
		queue:          make(chan internalCensus, 100),
		onchainFetcher: onchainFetcher,
		storage:        stg,
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

	go func() {
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
			case icensus := <-cd.queue:
				// Check if census is already pending
				if _, pending := cd.DownloadCensusStatus(icensus.Census); pending {
					continue
				}
				// Add census to pending list
				cd.addPendingCensus(icensus.Census)
				// Process census download
				if err := cd.processCensusDownload(runCtx, icensus); err != nil {
					log.Warnw("census download failed", "census", icensus.Census, "err", err)
				}
			}
		}
	}()
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
}

// DownloadCensus adds the specified census to the download queue for
// asynchronous processing. It handles on-chain dynamic Merkle Tree censuses
// by fetching the current root from the on-chain contract before adding it to
// the queue. It returns the final census root (after any necessary updates)
// and an error if the operation fails.
func (cd *CensusDownloader) DownloadCensus(censusInfo *types.Census) (types.HexBytes, error) {
	runCtx, err := cd.downloaderContext()
	if err != nil {
		return nil, fmt.Errorf("census downloader unavailable: %w", err)
	}
	// Create the internal census structure
	icensus := internalCensus{
		Census:            censusInfo,
		ProcessedElements: 0,
	}
	// Add on-chain census to the on-chain census map if applicable
	if icensus.CensusOrigin == types.CensusOriginMerkleTreeOnchainDynamicV1 {
		if icensus.CensusRoot, err = cd.addOnchainCensus(icensus); err != nil {
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
// census. It returns the DownloadStatus and a boolean indicating whether the
// census is found in the pending list.
func (cd *CensusDownloader) DownloadCensusStatus(census *types.Census) (DownloadStatus, bool) {
	cd.mu.RLock()
	defer cd.mu.RUnlock()
	status, exists := cd.censusStatus[censusKey(census)]
	return status, exists
}

// OnCensusDownloaded registers a callback function that will be called when
// the specified census has been downloaded and imported. The callback will
// be called with an error if the download or import failed, or nil if it
// succeeded. It allows to wait asynchronously for a census to be ready, and
// then execute custom logic based on the result.
func (cd *CensusDownloader) OnCensusDownloaded(census *types.Census, ctx context.Context, callback func(error)) {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		innerCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		for {
			select {
			case <-innerCtx.Done():
				callback(fmt.Errorf("context done before census downloaded"))
				return
			case <-ticker.C:
				// Get the current download status of the census
				status, exists := cd.DownloadCensusStatus(census)
				// If the census is not found in the pending list, it means it
				// was never queued for download or it was cleaned up after
				// completion/failure, so we can return an error
				if !exists {
					callback(fmt.Errorf("census not found in pending list"))
					return
				}
				// Return the last error if the downloader has reached the
				// maximum number of attempts.
				if status.LastErr != nil && status.Attempts >= cd.attempts() {
					callback(status.LastErr)
					return
				}
				// If the census download is complete, clean up the pending
				// status and call the callback with nil error
				if status.Complete {
					cd.CleanUp(status.census)
					callback(nil)
					return
				}
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

// processCensusDownload attempts to download and import the given census. It
// retries the download and import process up to the configured number of
// attempts. After each attempt, it updates the status of the census in the
// internal tracking map.
func (cd *CensusDownloader) processCensusDownload(ctx context.Context, census internalCensus) error {
	var importErr error
	for attempt := 0; attempt < cd.attempts(); attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("census download canceled: %w", err)
		}
		initialProcessedElements := census.ProcessedElements

		attemptCtx := ctx
		cancel := func() {}
		if cd.config.AttemptTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, cd.config.AttemptTimeout)
		}
		census.ProcessedElements, importErr = cd.importer.ImportCensus(attemptCtx, census.Census, census.ProcessedElements)
		cancel()
		cd.updateInternalStatus(census, importErr)
		if importErr == nil {
			if newElements := census.ProcessedElements - initialProcessedElements; newElements > 0 {
				log.Infow("census imported successfully",
					"attempt", attempt+1,
					"root", census.CensusRoot.String(),
					"uri", census.CensusURI,
					"newElements", census.ProcessedElements-initialProcessedElements,
					"origin", census.CensusOrigin.String())
			}
			return nil
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
// censuses.
func (cd *CensusDownloader) addPendingCensus(census *types.Census) {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	key := censusKey(census)
	if _, exists := cd.censusStatus[key]; exists {
		return
	}
	cd.censusStatus[key] = DownloadStatus{
		Attempts: 0,
		census:   census,
	}
}

func (cd *CensusDownloader) addOnchainCensus(icensus internalCensus) (types.HexBytes, error) {
	// Convert the root to a contract address and validate it
	if icensus.ContractAddress == (common.Address{}) {
		return nil, fmt.Errorf("invalid on-chain census contract address")
	}
	// Fetch the current census root from the on-chain contract and update
	// it in the original census
	var err error
	icensus.CensusRoot, err = cd.onchainFetcher.FetchOnchainCensusRoot(icensus.ContractAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch on-chain census root: %w", err)
	}
	// Store the on-chain census information for later processing
	cd.onchainCensuses.Store(icensus.ContractAddress.String(), icensus)
	return icensus.CensusRoot, nil
}

// updateInternalStatus updates the status of a pending census download
// attempt. It returns true if the census was found and updated, false
// otherwise.
func (cd *CensusDownloader) updateInternalStatus(icensus internalCensus, err error) {
	// Update also on-chain census map if applicable
	if icensus.CensusOrigin == types.CensusOriginMerkleTreeOnchainDynamicV1 {
		current, ok := cd.loadOnchainCensus(icensus.ContractAddress)
		if ok && current.ProcessedElements < icensus.ProcessedElements {
			cd.onchainCensuses.Swap(icensus.ContractAddress.String(), icensus)
		}
	}

	// Lock the mutex to update the census status map
	cd.mu.Lock()
	defer cd.mu.Unlock()

	// Ensure the census exists in the pending map
	key := censusKey(icensus.Census)
	if status, exists := cd.censusStatus[key]; exists {
		// Update the status with the current attempt results
		status.lastUpdated = time.Now()
		status.Complete = err == nil
		status.Attempts++
		if status.Attempts < cd.config.Attempts {
			status.LastErr = err
		} else if err != nil {
			status.LastErr = fmt.Errorf("maximum attempts reached: %w", err)
		}
		cd.censusStatus[key] = status
	}
}

// checkOnchainCensuses re-try to download on-chain dynamic Merkle Tree censuses
// looking for apply any updates that may have occurred on-chain.
func (cd *CensusDownloader) checkOnchainCensuses() {
	cd.onchainCensuses.Range(func(key, value any) bool {
		icensus, ok := value.(internalCensus)
		if !ok {
			return true
		}
		// Skip those that are already downloading and not complete
		if status, exists := cd.censusStatus[icensus.CensusRoot.String()]; exists && !status.Complete {
			return true
		}
		// Add on-chain census to the on-chain census map if applicable
		var err error
		if icensus.CensusRoot, err = cd.addOnchainCensus(icensus); err != nil {
			log.Warnw("failed to add on-chain census", "address", icensus.ContractAddress.Hex(), "error", err)
			return true
		}
		cd.queue <- icensus
		return true
	})
}

func (cd *CensusDownloader) loadOnchainCensus(address common.Address) (internalCensus, bool) {
	value, ok := cd.onchainCensuses.Load(address.String())
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
		if status.lastUpdated.Add(cd.config.Expiration).Before(now) {
			cd.cleanUpStatusUnsafe(status.census)
		}
	}
}

// CleanUp is a thread-safe wrapper around cleanUpStatusUnsafe that locks
// the mutex before calling the unsafe version to remove a census status from
// the internal tracking map based on the given census root.
func (cd *CensusDownloader) CleanUp(census *types.Census) {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	cd.cleanUpStatusUnsafe(census)
}

// cleanUpStatusUnsafe removes a census status from the internal tracking
// map based on the given census root. The caller should ensure that holds
// the mutex lock before calling this function to avoid data races.
func (cd *CensusDownloader) cleanUpStatusUnsafe(census *types.Census) {
	delete(cd.censusStatus, censusKey(census))
}

// censusKey generates a unique key for a census based on its root hash. This
// key is used for tracking the status of census downloads in the internal
// map.
func censusKey(census *types.Census) string {
	return census.CensusRoot.String()
}
