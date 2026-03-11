package service

import (
	"context"

	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/results"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"golang.org/x/sync/errgroup"
)

// DownloadArtifacts downloads all the circuit artifacts concurrently.
func DownloadArtifacts(ctx context.Context, dataDir string) error {
	if dataDir != "" {
		circuits.BaseDir = dataDir
	}
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return voteverifier.Artifacts.Download(ctx) })
	g.Go(func() error { return ballotproof.Artifacts.Download(ctx) })
	g.Go(func() error { return aggregator.Artifacts.Download(ctx) })
	g.Go(func() error { return statetransition.Artifacts.Download(ctx) })
	g.Go(func() error { return results.Artifacts.Download(ctx) })
	return g.Wait()
}

func DownloadWorkerArtifacts(ctx context.Context, dataDir string) error {
	if dataDir != "" {
		circuits.BaseDir = dataDir
	}
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return voteverifier.Artifacts.Download(ctx) })
	g.Go(func() error { return ballotproof.Artifacts.Download(ctx) })
	return g.Wait()
}
