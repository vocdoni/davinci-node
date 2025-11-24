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

type CensusDownloaderConfig struct {
	Interval   time.Duration
	Expiration time.Duration
	Cooldown   time.Duration
	Attempts   int
}

type downloadStatus struct {
	census      *types.Census
	complete    bool
	attempts    int
	lastErr     error
	lastUpdated time.Time
}

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

func NewCensusDownloader(contracts ContractsService, stg *storage.Storage, config CensusDownloaderConfig) *CensusDownloader {
	return &CensusDownloader{
		DownloadQueue: make(chan *types.Census),
		contracts:     contracts,
		storage:       stg,
		importer:      census.NewCensusImporter(stg),
		config:        config,
	}
}

func (cd *CensusDownloader) Start(ctx context.Context) error {
	cd.ctx, cd.cancel = context.WithCancel(ctx)

	go func() {
		cleanUpTicker := time.NewTicker(cd.config.Interval)
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

func (cd *CensusDownloader) Stop() {
	if cd.cancel != nil {
		cd.cancel()
	}
}

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

func (cd *CensusDownloader) IsCensusPending(census *types.Census) bool {
	cd.mu.RLock()
	defer cd.mu.RUnlock()
	censusKey := census.CensusRoot.String()
	_, exists := cd.pendingCensuses[censusKey]
	return exists
}
