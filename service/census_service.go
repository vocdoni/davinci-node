package service

import (
	"context"
	"fmt"
	"sync"
	"time"

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

// downloadStatus holds the status of a census download attempt. It is for
// internal use by the CensusDownloader.
type downloadStatus struct {
	census      *types.Census
	complete    bool
	attempts    int
	lastErr     error
	lastUpdated time.Time
}

// CensusDownloader is responsible for downloading and importing censuses
// asynchronously. It maintains a queue of censuses to download and tracks
// the status of each download attempt.
type CensusDownloader struct {
	DownloadQueue   chan *types.Census
	config          CensusDownloaderConfig
	ctx             context.Context
	cancel          context.CancelFunc
	contracts       ContractsService
	storage         *storage.Storage
	importer        *census.CensusImporter
	pendingCensuses map[string]downloadStatus
	mu              sync.RWMutex
}

// NewCensusDownloader creates a new CensusDownloader instance with the given
// ContractsService, Storage, and configuration.
func NewCensusDownloader(
	contracts ContractsService,
	stg *storage.Storage,
	config CensusDownloaderConfig,
) *CensusDownloader {
	return &CensusDownloader{
		DownloadQueue: make(chan *types.Census),
		contracts:     contracts,
		storage:       stg,
		importer:      census.NewCensusImporter(stg),
		config:        config,
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
			case census := <-cd.DownloadQueue:
				// Check if census is already pending
				if cd.IsCensusPending(census) {
					log.Warnw("census already pending",
						"census", census.CensusRoot.String(),
						"uri", census.CensusURI,
						"origin", census.CensusOrigin.String())
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

// cleanUpPendingCensuses removes expired pending censuses from the internal
// tracking map.
func (cd *CensusDownloader) cleanUpPendingCensuses() {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	now := time.Now()
	for key, status := range cd.pendingCensuses {
		if status.lastUpdated.Add(cd.config.Expiration).Before(now) {
			delete(cd.pendingCensuses, key)
		}
	}
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

	for attempt := 0; attempt <= cd.config.Attempts; attempt++ {
		importErr := cd.importer.ImportCensus(ctx, census)
		if ok := cd.updatePendingCensusStatus(census, importErr); !ok {
			return fmt.Errorf("failed to store census status")
		}
		if importErr != nil {
			log.Warnw("census import attempt failed",
				"attempt", attempt+1,
				"error", importErr,
				"root", census.CensusRoot.String(),
				"uri", census.CensusURI,
				"origin", census.CensusOrigin.String())
		} else {
			log.Infow("census imported successfully",
				"root", census.CensusRoot.String(),
				"uri", census.CensusURI,
				"origin", census.CensusOrigin.String())
			return nil
		}
	}
	return nil
}

// addPendingCensus adds a census to the internal tracking map of pending
// censuses.
func (cd *CensusDownloader) addPendingCensus(census *types.Census) {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	if cd.pendingCensuses == nil {
		cd.pendingCensuses = make(map[string]downloadStatus)
	}
	censusKey := census.CensusRoot.String()
	if _, exists := cd.pendingCensuses[censusKey]; exists {
		return
	}
	cd.pendingCensuses[censusKey] = downloadStatus{
		attempts: 0,
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
	status, exists := cd.pendingCensuses[censusKey]
	if !exists {
		return false
	}
	status.attempts++
	status.lastErr = err
	status.lastUpdated = time.Now()
	status.complete = err == nil
	cd.pendingCensuses[censusKey] = status
	return true
}

// IsCensusPending checks if a census is currently pending download or import.
// It returns true if the census is pending, false otherwise.
func (cd *CensusDownloader) IsCensusPending(census *types.Census) bool {
	cd.mu.RLock()
	defer cd.mu.RUnlock()
	censusKey := census.CensusRoot.String()
	_, exists := cd.pendingCensuses[censusKey]
	return exists
}
