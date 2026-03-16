package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/tests/helpers"
	"github.com/vocdoni/davinci-node/workers"
)

var (
	services          *helpers.TestServices
	defaultBallotMode = testutil.BallotMode()
)

const artifactsTimeout = 20 * time.Minute

func TestMain(m *testing.M) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" || os.Getenv("RUN_INTEGRATION_TESTS") == "false" {
		log.Info("skipping integration tests...")
		os.Exit(0)
	}

	log.Init(log.LogLevelDebug, "stdout", nil)
	tempDir := os.TempDir() + "/davinci-node-test-" + time.Now().Format("20060102150405")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	downloadCtx, downloadCancel := context.WithTimeout(ctx, artifactsTimeout)
	defer downloadCancel()

	if err := service.DownloadArtifacts(downloadCtx, ""); err != nil {
		log.Fatalf("failed to download artifacts: %v", err)
	}

	var err error
	var cleanup func()
	services, cleanup, err = helpers.NewTestServices(ctx, tempDir,
		helpers.WorkerSeed,
		helpers.WorkerTokenExpiration,
		helpers.WorkerTimeout,
		workers.DefaultWorkerBanRules)
	if err != nil {
		log.Fatalf("failed to setup test services: %v", err)
	}

	code := m.Run()

	cancel()

	cleanupDone := make(chan struct{})
	go func() {
		cleanup()
		close(cleanupDone)
	}()

	select {
	case <-cleanupDone:
		if err := os.RemoveAll(tempDir); err != nil {
			log.Fatalf("failed to remove temp dir (%s): %v", tempDir, err)
		}
	case <-time.After(30 * time.Second):
		log.Warn("cleanup timed out, forcing exit")
	}

	os.Exit(code)
}
