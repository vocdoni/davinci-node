package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/workers"
)

const (
	testWorkerSeed            = "test-seed"
	testWorkerTokenExpiration = 24 * time.Hour
	testWorkerTimeout         = time.Second * 5
)

var (
	orgAddr  common.Address
	services *Services
)

func TestMain(m *testing.M) {
	log.Init(log.LogLevelDebug, "stdout", nil)
	if err := service.DownloadArtifacts(30*time.Minute, ""); err != nil {
		log.Fatalf("failed to download artifacts: %v", err)
	}

	tempDir := os.TempDir() + "/davinci-node-test-" + time.Now().Format("20060102150405")

	ctx, cancel := context.WithCancel(context.Background())

	var err error
	var cleanup func()
	services, cleanup, err = NewTestService(ctx, tempDir, testWorkerSeed, testWorkerTokenExpiration, testWorkerTimeout, workers.DefaultWorkerBanRules)
	if err != nil {
		log.Fatalf("failed to setup test services: %v", err)
	}

	// create organization
	if orgAddr, err = createOrganization(services.Contracts); err != nil {
		log.Fatalf("failed to create organization: %v", err)
	}
	log.Infof("Organization address: %s", orgAddr.String())

	code := m.Run()

	cancel()

	cleanupDone := make(chan struct{})
	go func() {
		cleanup()
		close(cleanupDone)
	}()

	select {
	case <-cleanupDone:
	case <-time.After(30 * time.Second):
		log.Warn("cleanup timed out, forcing exit")
	}

	if err := os.RemoveAll(tempDir); err != nil {
		log.Fatalf("failed to remove temp dir (%s): %v", tempDir, err)
	}
	os.Exit(code)
}
