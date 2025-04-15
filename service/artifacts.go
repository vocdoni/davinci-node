package service

import (
	"context"
	"time"

	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/aggregator"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/ballotproof"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/statetransition"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/voteverifier"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
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
	log.Infow("preparing zkSNARK circuit artifacts", "timeout", timeout, "dataDir", dataDir)
	return g.Wait()
}
