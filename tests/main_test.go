package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/vocdoni/davinci-node/api/client"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/service"
	"github.com/vocdoni/davinci-node/workers"
)

const (
	testWorkerSeed        = "test-seed"      // seed for the workers UUID of main sequencer
	workerTimeout         = 5 * time.Second  // timeout for worker jobs
	failedJobsToGetBanned = 3                // number of failed jobs to get banned
	workerBanTimeout      = 30 * time.Second // timeout for worker ban
	// first account private key created by anvil with default mnemonic
	testLocalAccountPrivKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
)

var (
	services *testServices
	cli      *client.HTTPclient
)

func TestMain(m *testing.M) {
	log.Init(log.LogLevelDebug, "stdout", nil)
	if err := service.DownloadArtifacts(30*time.Minute, ""); err != nil {
		log.Errorw(err, "failed to download artifacts")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Create a test service with the specified worker seed and timeout
	var err error
	services, err = newTestServices(ctx, testWorkerSeed, workerTimeout, &workers.WorkerBanRules{
		BanTimeout:          workerBanTimeout,
		FailuresToGetBanned: failedJobsToGetBanned,
	})
	// Start sequencer batch time window
	services.sequencer.SetBatchTimeWindow(time.Second * 120)
	if err != nil {
		log.Errorw(err, "failed to create test services")
		os.Exit(1)
	}
	defer services.Stop()
	// Create a test client using the API host port
	_, port := services.api.HostPort()
	cli, err = newTestAPIClient(port)
	if err != nil {
		log.Errorw(err, "failed to create test client")
		os.Exit(1)
	}
	os.Exit(m.Run())
}
