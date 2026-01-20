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
	CleanUpInterval time.Duration
	Expiration      time.Duration
	Cooldown        time.Duration
	Attempts        int
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
	queue          chan *types.Census
	config         CensusDownloaderConfig
	ctx            context.Context
	cancel         context.CancelFunc
	onchainFetcher OnchainCensusFetcher
	storage        *storage.Storage
	importer       *census.CensusImporter
	censusStatus   map[string]DownloadStatus
	mu             sync.RWMutex
}

// NewCensusDownloader creates a new CensusDownloader instance with the given
// ContractsService, Storage, and configuration.
func NewCensusDownloader(
	onchainFetcher OnchainCensusFetcher,
	stg *storage.Storage,
	config CensusDownloaderConfig,
) *CensusDownloader {
	return &CensusDownloader{
		queue:          make(chan *types.Census),
		onchainFetcher: onchainFetcher,
		storage:        stg,
		importer: census.NewCensusImporter(
			stg,
			census.JSONImporter(),
			census.GraphQLImporter(nil),
		),
		censusStatus: make(map[string]DownloadStatus),
		config:       config,
	}
}

// Start begins the CensusDownloader's processing loop. It listens for new
// censuses to download from the DownloadQueue channel and processes them
// asynchronously. If the context is canceled, the downloader stops processing.
func (cd *CensusDownloader) Start(ctx context.Context) error {
	cd.ctx, cd.cancel = context.WithCancel(ctx)

	go func() {
		cleanUpTicker := time.NewTicker(cd.config.CleanUpInterval)
		defer cleanUpTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case census := <-cd.queue:
				// Check if census is already pending
				if _, pending := cd.DownloadCensusStatus(census); pending {
					continue
				}
				// Add census to pending list
				cd.addPendingCensus(census)
				// Process census download
				if err := cd.processCensusDownload(ctx, census); err != nil {
					time.Sleep(cd.config.Cooldown)
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
// the queue. It returns the final census root (after any necessary updates) and
// an error if the operation fails.
func (cd *CensusDownloader) DownloadCensus(census *types.Census) (types.HexBytes, error) {
	// Handle on-chain dynamic Merkle Tree censuses
	if census.CensusOrigin == types.CensusOriginMerkleTreeOnchainDynamicV1 {
		// Convert the root to a contract address and validate it
		contractAddress := common.BytesToAddress(census.CensusRoot.RightTrim())
		if contractAddress == (common.Address{}) {
			return nil, fmt.Errorf("invalid on-chain census contract address")
		}
		// Fetch the current census root from the on-chain contract and update
		// it in the original census
		var err error
		census.CensusRoot, err = cd.onchainFetcher.FetchOnchainCensusRoot(contractAddress)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch on-chain census root: %w", err)
		}
	}
	// Add the census to the queue to be downloaded
	cd.queue <- census
	return census.CensusRoot, nil
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
func (cd *CensusDownloader) processCensusDownload(ctx context.Context, census *types.Census) error {
	log.Infow("starting census download",
		"root", census.CensusRoot.String(),
		"uri", census.CensusURI,
		"origin", census.CensusOrigin.String())

	var importErr error
	for attempt := 0; attempt <= cd.config.Attempts; attempt++ {
		importErr = cd.importer.ImportCensus(ctx, census)
		if ok := cd.updatePendingCensusStatus(census, importErr); !ok {
			return fmt.Errorf("failed to store census status")
		}
		if importErr == nil {
			log.Infow("census imported successfully",
				"attempt", attempt+1,
				"root", census.CensusRoot.String(),
				"uri", census.CensusURI,
				"origin", census.CensusOrigin.String())
			return nil
		}
	}

	log.Warnw("census import failed",
		"error", importErr,
		"attempts", cd.config.Attempts,
		"root", census.CensusRoot.String(),
		"uri", census.CensusURI,
		"origin", census.CensusOrigin.String())
	return importErr
}

// addPendingCensus adds a census to the internal tracking map of pending
// censuses.
func (cd *CensusDownloader) addPendingCensus(census *types.Census) {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	if cd.censusStatus == nil {
		cd.censusStatus = make(map[string]DownloadStatus)
	}
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
