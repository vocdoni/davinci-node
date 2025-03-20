package service

import (
	"context"
	"time"

	"github.com/vocdoni/vocdoni-z-sandbox/circuits/aggregator"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/ballotproof"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/voteverifier"
	"golang.org/x/sync/errgroup"
)

// DownloadArtifacts downloads all the circuit artifacts concurrently.
func DownloadArtifacts(timeout time.Duration) error {
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
	return g.Wait()
}
