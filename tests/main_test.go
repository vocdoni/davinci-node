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
		log.Errorw(err, "failed to download artifacts")
		return
	}

	// create a temp dir
	tempDir := os.TempDir() + "/davinci-node-test-" + time.Now().Format("20060102150405")
	// defer the removal of the temp dir
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			log.Warnw("failed to remove temp dir", "dir", tempDir, "error", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var err error
	var cleanup func()
	services, cleanup, err = NewTestService(ctx, tempDir, testWorkerSeed, testWorkerTokenExpiration, testWorkerTimeout, workers.DefaultWorkerBanRules)
	if err != nil {
		log.Errorw(err, "failed to setup test services")
		return
	}

	// create organization
	if orgAddr, err = createOrganization(services.Contracts); err != nil {
		log.Errorw(err, "failed to create organization")
		return
	}
	log.Infof("Organization address: %s", orgAddr.String())

	// Defer cleanup to run after tests complete
	defer cleanup()
	os.Exit(m.Run())
}
