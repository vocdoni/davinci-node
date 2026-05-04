package web3

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
)

func TestNewNetworkRuntime(t *testing.T) {
	c := qt.New(t)

	contracts := &Contracts{
		ChainID: 11155111,
		ContractsAddresses: &Addresses{
			ProcessRegistry: common.HexToAddress("0x015eac820688da203a0bd730a8a7a4cdb97e1a02"),
		},
	}

	runtime, err := NewNetworkRuntime("sepolia", contracts, nil)

	c.Assert(err, qt.IsNil)
	c.Assert(runtime.Network, qt.Equals, "sepolia")
	c.Assert(runtime.Contracts, qt.Equals, contracts)
	c.Assert(runtime.TxManager, qt.IsNil)
	c.Assert(runtime.ProcessIDVersion, qt.DeepEquals,
		types.ProcessIDVersion(uint32(contracts.ChainID), contracts.ContractsAddresses.ProcessRegistry))
}

func TestNewNetworkRuntimeErrors(t *testing.T) {
	c := qt.New(t)

	validContracts := &Contracts{
		ChainID: 1,
		ContractsAddresses: &Addresses{
			ProcessRegistry: common.HexToAddress("0x0000000000000000000000000000000000000001"),
		},
	}

	c.Run("missing network", func(c *qt.C) {
		runtime, err := NewNetworkRuntime("", validContracts, nil)
		c.Assert(runtime, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "network is required")
	})

	c.Run("missing contracts", func(c *qt.C) {
		runtime, err := NewNetworkRuntime("mainnet", nil, nil)
		c.Assert(runtime, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "contracts is required")
	})

	c.Run("missing addresses", func(c *qt.C) {
		runtime, err := NewNetworkRuntime("mainnet", &Contracts{ChainID: 1}, nil)
		c.Assert(runtime, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "contracts addresses are required")
	})

	c.Run("missing process registry", func(c *qt.C) {
		runtime, err := NewNetworkRuntime("mainnet", &Contracts{
			ChainID:            1,
			ContractsAddresses: &Addresses{},
		}, nil)
		c.Assert(runtime, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "process registry address is required")
	})

	c.Run("chain ID exceeds version limit", func(c *qt.C) {
		runtime, err := NewNetworkRuntime("oversized", &Contracts{
			ChainID: maxProcessIDChainID + 1,
			ContractsAddresses: &Addresses{
				ProcessRegistry: common.HexToAddress("0x0000000000000000000000000000000000000001"),
			},
		}, nil)
		c.Assert(runtime, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "exceeds ProcessIDVersion limit")
	})
}

func TestNewRuntimeRouter(t *testing.T) {
	c := qt.New(t)

	sepoliaRuntime := testRuntime(c, "sepolia", 11155111, "0x015eac820688da203a0bd730a8a7a4cdb97e1a02")
	celoRuntime := testRuntime(c, "celo", 42220, "0x68dac70af68aa0bed8cef36c523243941d7d7876")

	router, err := NewRuntimeRouter(sepoliaRuntime, celoRuntime)

	c.Assert(err, qt.IsNil)
	c.Assert(router.Runtimes(), qt.HasLen, 2)

	runtime, ok := router.RuntimeForVersion(sepoliaRuntime.ProcessIDVersion)
	c.Assert(ok, qt.IsTrue)
	c.Assert(runtime, qt.Equals, sepoliaRuntime)

	processID := types.NewProcessID(
		common.HexToAddress("0x1234567890123456789012345678901234567890"),
		celoRuntime.ProcessIDVersion,
		7,
	)
	contracts, err := router.ContractsForProcess(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(contracts, qt.Equals, celoRuntime.Contracts)

	blobFetcher, err := router.BlobFetcherForProcess(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(blobFetcher, qt.Equals, celoRuntime.Contracts)
}

func TestNewRuntimeRouterRejectsDuplicateVersions(t *testing.T) {
	c := qt.New(t)

	processRegistry := "0x015eac820688da203a0bd730a8a7a4cdb97e1a02"
	runtimeA := testRuntime(c, "sepolia-a", 11155111, processRegistry)
	runtimeB := testRuntime(c, "sepolia-b", 11155111, processRegistry)

	router, err := NewRuntimeRouter(runtimeA, runtimeB)

	c.Assert(router, qt.IsNil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "duplicate ProcessIDVersion")
}

func TestRuntimeRouterContractsForProcessErrors(t *testing.T) {
	c := qt.New(t)

	router, err := NewRuntimeRouter(testRuntime(c, "sepolia", 11155111, "0x015eac820688da203a0bd730a8a7a4cdb97e1a02"))
	c.Assert(err, qt.IsNil)

	contracts, err := router.ContractsForProcess(types.ProcessID{})
	c.Assert(contracts, qt.IsNil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "invalid process ID")

	unknownProcess := types.NewProcessID(
		common.HexToAddress("0x1234567890123456789012345678901234567890"),
		[4]byte{0xde, 0xad, 0xbe, 0xef},
		1,
	)
	contracts, err = router.ContractsForProcess(unknownProcess)
	c.Assert(contracts, qt.IsNil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "runtime not found")
}

func testRuntime(c *qt.C, network string, chainID uint64, processRegistry string) *NetworkRuntime {
	contracts := &Contracts{
		ChainID: chainID,
		ContractsAddresses: &Addresses{
			ProcessRegistry: common.HexToAddress(processRegistry),
		},
	}
	runtime, err := NewNetworkRuntime(network, contracts, nil)
	c.Assert(err, qt.IsNil)
	return runtime
}
