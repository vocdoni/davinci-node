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
//   - Attempts: maximum number of attempts to download and import a census.
type CensusDownloaderConfig struct {
	CleanUpInterval      time.Duration
	OnchainCheckInterval time.Duration
	Expiration           time.Duration
	Cooldown             time.Duration
	Attempts             int
}

// DefaultCensusDownloaderConfig provides default values for the CensusDownloaderConfig.
var DefaultCensusDownloaderConfig = CensusDownloaderConfig{
	CleanUpInterval:      time.Second * 5,
	OnchainCheckInterval: time.Second * 15,
	Attempts:             5,
	Expiration:           time.Minute * 10,
	Cooldown:             time.Second * 10,
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
	cd.ctx, cd.cancel = context.WithCancel(ctx)

	go func() {
		// Tickers for periodic tasks: onchain census checks and cleanup of
		// pending censuses
		onchainCheckTicker := time.NewTicker(cd.config.OnchainCheckInterval)
		defer onchainCheckTicker.Stop()
		cleanUpTicker := time.NewTicker(cd.config.CleanUpInterval)
		defer cleanUpTicker.Stop()

		for {
			select {
			case <-ctx.Done():
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
				if err := cd.processCensusDownload(ctx, icensus); err != nil {
					time.Sleep(cd.config.Cooldown)
				}
			}
		}
	}()
	return nil
}

// Stop stops the CensusDownloader's processing loop by canceling its context.
func (cd *CensusDownloader) Stop() {
	if cd.cancel != nil {
		cd.cancel()
	}
}

// DownloadCensus adds the specified census to the download queue for
// asynchronous processing. It handles on-chain dynamic Merkle Tree censuses
// by fetching the current root from the on-chain contract before adding it to
// the queue. It returns the final census root (after any necessary updates)
// and an error if the operation fails.
func (cd *CensusDownloader) DownloadCensus(censusInfo *types.Census) (types.HexBytes, error) {
	// Create the internal census structure
	icensus := internalCensus{
		Census:            censusInfo,
		ProcessedElements: 0,
	}
	// Add on-chain census to the on-chain census map if applicable
	if censusInfo.CensusOrigin == types.CensusOriginMerkleTreeOnchainDynamicV1 {
		if err := cd.addOnchainCensus(icensus); err != nil {
			return nil, fmt.Errorf("failed to add on-chain census: %w", err)
		}
	}
	// Add the census to the queue to be downloaded
	log.Infow("starting census download",
		"root", censusInfo.CensusRoot.String(),
		"uri", censusInfo.CensusURI,
		"origin", censusInfo.CensusOrigin.String())
	cd.queue <- icensus
	return censusInfo.CensusRoot, nil
}

// DownloadCensusStatus retrieves the current download status of the specified
// census. It returns the DownloadStatus and a boolean indicating whether the
// census is found in the pending list.
func (cd *CensusDownloader) DownloadCensusStatus(census *types.Census) (DownloadStatus, bool) {
	cd.mu.RLock()
	defer cd.mu.RUnlock()
	censusKey := census.CensusRoot.String()
	status, exists := cd.censusStatus[censusKey]
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
				status, exists := cd.DownloadCensusStatus(census)
				if !exists {
					callback(fmt.Errorf("census not found in pending list"))
					return
				}
				if status.Complete {
					callback(nil)
					return
				}
			}
		}
	}()
}

// processCensusDownload attempts to download and import the given census. It
// retries the download and import process up to the configured number of
// attempts. After each attempt, it updates the status of the census in the
// internal tracking map.
func (cd *CensusDownloader) processCensusDownload(ctx context.Context, censusInfo internalCensus) error {
	var importErr error
	for attempt := 0; attempt <= cd.config.Attempts; attempt++ {
		initialElements := censusInfo.ProcessedElements
		censusInfo.ProcessedElements, importErr = cd.importer.ImportCensus(ctx, censusInfo.Census, censusInfo.ProcessedElements)
		cd.updateInternalStatus(censusInfo, importErr)
		if importErr == nil {
			if newElements := censusInfo.ProcessedElements - initialElements; newElements > 0 {
				log.Infow("census imported successfully",
					"attempt", attempt+1,
					"root", censusInfo.CensusRoot.String(),
					"uri", censusInfo.CensusURI,
					"newElements", censusInfo.ProcessedElements-initialElements,
					"origin", censusInfo.CensusOrigin.String())
			}
			return nil
		}
	}

	log.Warnw("census import failed",
		"error", importErr,
		"attempts", cd.config.Attempts,
		"root", censusInfo.CensusRoot.String(),
		"uri", censusInfo.CensusURI,
		"origin", censusInfo.CensusOrigin.String())
	return importErr
}

// addPendingCensus adds a census to the internal tracking map of pending
// censuses.
func (cd *CensusDownloader) addPendingCensus(census *types.Census) {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	censusKey := census.CensusRoot.String()
	if _, exists := cd.censusStatus[censusKey]; exists {
		return
	}
	cd.censusStatus[censusKey] = DownloadStatus{
		Attempts: 0,
		census:   census,
	}
}

func (cd *CensusDownloader) addOnchainCensus(icensus internalCensus) error {
	// Convert the root to a contract address and validate it
	if icensus.ContractAddress == (common.Address{}) {
		return fmt.Errorf("invalid on-chain census contract address")
	}
	// Fetch the current census root from the on-chain contract and update
	// it in the original census
	var err error
	icensus.CensusRoot, err = cd.onchainFetcher.FetchOnchainCensusRoot(icensus.ContractAddress)
	if err != nil {
		return fmt.Errorf("failed to fetch on-chain census root: %w", err)
	}
	// Store the on-chain census information for later processing
	cd.onchainCensuses.Store(icensus.ContractAddress.String(), icensus)
	return nil
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
	censusKey := icensus.CensusRoot.String()
	if status, exists := cd.censusStatus[censusKey]; exists {
		// Update the status with the current attempt results
		status.lastUpdated = time.Now()
		status.Complete = err == nil
		status.Attempts++
		if status.Attempts < cd.config.Attempts {
			status.LastErr = err
		} else if err != nil {
			status.LastErr = fmt.Errorf("maximum attempts reached: %w", err)
		}
		cd.censusStatus[censusKey] = status
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
	for key, status := range cd.censusStatus {
		if status.lastUpdated.Add(cd.config.Expiration).Before(now) {
			delete(cd.censusStatus, key)
		}
	}
}
