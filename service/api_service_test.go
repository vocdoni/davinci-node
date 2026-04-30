package service

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/metadata"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/web3"
)

func TestAPIService(t *testing.T) {
	c := qt.New(t)

	// Setup storage
	kv := memdb.New()
	store := storage.New(kv)
	defer store.Close()

	contracts := &web3.Contracts{
		ChainID: 11155111,
		ContractsAddresses: &web3.Addresses{
			ProcessRegistry:           common.HexToAddress(config.TestConfig.ProcessRegistrySmartContract),
			StateTransitionZKVerifier: common.HexToAddress(config.TestConfig.StateTransitionZKVerifier),
			ResultsZKVerifier:         common.HexToAddress(config.TestConfig.ResultsZKVerifier),
		},
	}
	runtime, err := web3.NewNetworkRuntime("test", contracts, nil)
	c.Assert(err, qt.IsNil)
	runtimes, err := web3.NewRuntimeRouter(runtime)
	c.Assert(err, qt.IsNil)

	// Create API service with a random available port
	apiService := NewAPI(store, "127.0.0.1", 0, runtimes, metadata.PinataMetadataProviderConfig{}, false) // Port 0 lets the OS choose an available port

	// Start service in background
	ctx := context.Background()

	err = apiService.Start(ctx)
	c.Assert(err, qt.IsNil)
	defer apiService.Stop()

	// Give the service time to start
	time.Sleep(2 * time.Second)

	// Test stopping and restarting
	apiService.Stop()
	err = apiService.Start(ctx)
	c.Assert(err, qt.IsNil)

	// Test starting an already running service
	err = apiService.Start(ctx)
	c.Assert(err, qt.ErrorMatches, "service already running")
}
