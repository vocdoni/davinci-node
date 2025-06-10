package service

import (
	"context"
	"time"

	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/results"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/log"
	"golang.org/x/sync/errgroup"
)

// DownloadArtifacts downloads all the circuit artifacts concurrently.
func DownloadArtifacts(timeout time.Duration, dataDir string) error {
	if dataDir != "" {
		circuits.BaseDir = dataDir
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return voteverifier.Artifacts.DownloadAll(ctx)
	})
	g.Go(func() error {
		return ballotproof.Artifacts.DownloadAll(ctx)
	})
	g.Go(func() error {
		return aggregator.Artifacts.DownloadAll(ctx)
	})
	g.Go(func() error {
		return statetransition.Artifacts.DownloadAll(ctx)
	})
	g.Go(func() error {
		return results.Artifacts.DownloadAll(ctx)
	})
	log.Infow("preparing zkSNARK circuit full sequencer artifacts", "timeout", timeout, "dataDir", dataDir)
	return g.Wait()
}

func DownloadWorkerArtifacts(timeout time.Duration, dataDir string) error {
	if dataDir != "" {
		circuits.BaseDir = dataDir
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return voteverifier.Artifacts.DownloadAll(ctx)
	})
	g.Go(func() error {
		return ballotproof.Artifacts.DownloadAll(ctx)
	})
	log.Infow("preparing zkSNARK circuit worker artifacts", "timeout", timeout, "dataDir", dataDir)
	return g.Wait()
}
