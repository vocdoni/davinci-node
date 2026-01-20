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
	*census.CensusToUpdate
}

// onchainCensus holds information about an on-chain dynamic census. It includes
// the internal census data and the contract address where the census lives.
type onchainCensus struct {
	*internalCensus
	address common.Address
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
	queue           chan *internalCensus
	config          CensusDownloaderConfig
	ctx             context.Context
	cancel          context.CancelFunc
	onchainFetcher  OnchainCensusFetcher
	storage         *storage.Storage
	importer        *census.CensusImporter
	censusStatus    map[string]DownloadStatus
	onchainCensuses map[string]onchainCensus
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
		queue:          make(chan *internalCensus),
		onchainFetcher: onchainFetcher,
		storage:        stg,
		importer: census.NewCensusImporter(
			stg,
			census.JSONImporter(),
			census.GraphQLImporter(nil),
		),
		censusStatus:    make(map[string]DownloadStatus),
		onchainCensuses: make(map[string]onchainCensus),
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
			case <-onchainCheckTicker.C:
				// Check for updates to on-chain censuses
				if err := cd.checkOnchainCensuses(); err != nil {
					log.Warnw("failed to check on-chain censuses", "error", err)
				}
			case <-cleanUpTicker.C:
				cd.cleanUpPendingCensuses()
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
	icensus := &internalCensus{
		Census: censusInfo,
		CensusToUpdate: &census.CensusToUpdate{
			OldCensusRoot:     nil,
			ProcessedElements: 0,
		},
	}
	// Handle on-chain dynamic Merkle Tree censuses
	if censusInfo.CensusOrigin == types.CensusOriginMerkleTreeOnchainDynamicV1 {
		// Convert the root to a contract address and validate it
		contractAddress := common.BytesToAddress(censusInfo.CensusRoot.RightTrim())
		if contractAddress == (common.Address{}) {
			return nil, fmt.Errorf("invalid on-chain census contract address")
		}
		// Fetch the current census root from the on-chain contract and update
		// it in the original census
		var err error
		censusInfo.CensusRoot, err = cd.onchainFetcher.FetchOnchainCensusRoot(contractAddress)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch on-chain census root: %w", err)
		}
		log.Debugw("onchain census root unmasked",
			"contractAddress", contractAddress.String(),
			"censusRoot", censusInfo.CensusRoot.String())
		// Lock the mutex to update the on-chain census map
		cd.mu.Lock()
		// Store the on-chain census information for later processing
		cd.onchainCensuses[contractAddress.String()] = onchainCensus{
			internalCensus: icensus,
			address:        contractAddress,
		}
		cd.mu.Unlock()
	}
	// Add the census to the queue to be downloaded
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
func (cd *CensusDownloader) processCensusDownload(ctx context.Context, censusInfo *internalCensus) error {
	log.Infow("starting census download",
		"root", censusInfo.CensusRoot.String(),
		"uri", censusInfo.CensusURI,
		"origin", censusInfo.CensusOrigin.String())

	var importErr error
	for attempt := 0; attempt <= cd.config.Attempts; attempt++ {
		censusInfo.ProcessedElements, importErr = cd.importer.ImportCensus(ctx, censusInfo.Census, censusInfo.CensusToUpdate)
		if ok := cd.updatePendingCensusStatus(censusInfo.Census, importErr); !ok {
			return fmt.Errorf("failed to store census status")
		}
		if importErr == nil {
			log.Infow("census imported successfully",
				"attempt", attempt+1,
				"root", censusInfo.CensusRoot.String(),
				"uri", censusInfo.CensusURI,
				"origin", censusInfo.CensusOrigin.String())
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

// updatePendingCensusStatus updates the status of a pending census download
// attempt. It returns true if the census was found and updated, false
// otherwise.
func (cd *CensusDownloader) updatePendingCensusStatus(census *types.Census, err error) bool {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	censusKey := census.CensusRoot.String()
	status, exists := cd.censusStatus[censusKey]
	if !exists {
		return false
	}
	status.lastUpdated = time.Now()
	status.Complete = err == nil
	status.Attempts++
	if status.Attempts < cd.config.Attempts {
		status.LastErr = err
	} else if err != nil {
		status.LastErr = fmt.Errorf("maximum attempts reached: %w", err)
	}
	cd.censusStatus[censusKey] = status
	return true
}

// checkOnchainCensuses checks for updates to on-chain dynamic Merkle Tree
// censuses. If the census root has changed, it updates the internal census
// and enqueues it for download and import.
func (cd *CensusDownloader) checkOnchainCensuses() error {
	// Get current onchain censuses to be processed and don't block other
	// operations
	cd.mu.RLock()
	onchainCensuses := make(map[string]onchainCensus)
	for key, val := range cd.onchainCensuses {
		// Skipt those that are already downloading and not complete
		if status, exists := cd.censusStatus[val.CensusRoot.String()]; exists && !status.Complete {
			continue
		}
		// Ensure that if there is an old census root
		if val.OldCensusRoot != nil {
			if status, exists := cd.censusStatus[val.OldCensusRoot.String()]; exists && !status.Complete {
				continue
			}
		}
		onchainCensuses[key] = val
	}
	cd.mu.RUnlock()
	// Iterate over the on-chain censuses and check for updates
	for _, onchainCensus := range onchainCensuses {
		// Fetch the current census root from the on-chain contract
		root, err := cd.onchainFetcher.FetchOnchainCensusRoot(onchainCensus.address)
		if err != nil {
			return fmt.Errorf("failed to fetch on-chain census root: %w", err)
		}
		// Check if the root has changed
		if root.Equal(onchainCensus.CensusRoot) {
			continue
		}
		// Store the current census root as the old root and update to the new
		// root with the fetched value
		onchainCensus.OldCensusRoot = onchainCensus.CensusRoot
		onchainCensus.CensusRoot = root
		log.Debugw("onchain census root updated",
			"contractAddress", onchainCensus.address.String(),
			"newCensusRoot", onchainCensus.CensusRoot.String(),
			"oldCensusRoot", onchainCensus.OldCensusRoot.String())
		// Update the on-chain census in the internal map
		cd.mu.Lock()
		cd.onchainCensuses[onchainCensus.address.String()] = onchainCensus
		cd.mu.Unlock()
		// Enqueue the updated census for download
		// Add the census to the queue to be downloaded
		cd.queue <- onchainCensus.internalCensus
		log.Infow("on-chain census root updated, enqueuing for download",
			"address", onchainCensus.address.String(),
			"newRoot", root.String(),
			"oldRoot", onchainCensus.OldCensusRoot.String())
	}
	return nil
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
