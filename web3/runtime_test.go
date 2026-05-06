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

	runtime, err := NewNetworkRuntime(contracts, nil)

	c.Assert(err, qt.IsNil)
	c.Assert(runtime.ChainID, qt.Equals, uint64(11155111))
	c.Assert(runtime.Contracts, qt.Equals, contracts)
	c.Assert(runtime.TxManager, qt.IsNil)
	c.Assert(runtime.ProcessIDVersion, qt.DeepEquals,
		types.ProcessIDVersion(uint32(contracts.ChainID), contracts.ContractsAddresses.ProcessRegistry))
}

func TestNewNetworkRuntimeErrors(t *testing.T) {
	c := qt.New(t)

	c.Run("missing contracts", func(c *qt.C) {
		runtime, err := NewNetworkRuntime(nil, nil)
		c.Assert(runtime, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "contracts is required")
	})

	c.Run("missing addresses", func(c *qt.C) {
		runtime, err := NewNetworkRuntime(&Contracts{ChainID: 1}, nil)
		c.Assert(runtime, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "contracts addresses are required")
	})

	c.Run("missing process registry", func(c *qt.C) {
		runtime, err := NewNetworkRuntime(&Contracts{
			ChainID:            1,
			ContractsAddresses: &Addresses{},
		}, nil)
		c.Assert(runtime, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "process registry address is required")
	})

	c.Run("chain ID exceeds version limit", func(c *qt.C) {
		runtime, err := NewNetworkRuntime(&Contracts{
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

	sepoliaRuntime := testRuntime(c, 11155111, "0x015eac820688da203a0bd730a8a7a4cdb97e1a02")
	celoRuntime := testRuntime(c, 42220, "0x68dac70af68aa0bed8cef36c523243941d7d7876")

	router, err := NewRuntimeRouter(sepoliaRuntime, celoRuntime)

	c.Assert(err, qt.IsNil)
	c.Assert(router.Runtimes(), qt.HasLen, 2)

	runtime, ok := router.runtimeForVersion(sepoliaRuntime.ProcessIDVersion)
	c.Assert(ok, qt.IsTrue)
	c.Assert(runtime, qt.Equals, sepoliaRuntime)

	processID := types.NewProcessID(
		common.HexToAddress("0x1234567890123456789012345678901234567890"),
		celoRuntime.ProcessIDVersion,
		7,
	)
	c.Assert(router.SupportsProcess(processID), qt.IsTrue)
	runtime, err = router.RuntimeForProcess(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(runtime, qt.Equals, celoRuntime)

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
	runtimeA := testRuntime(c, 11155111, processRegistry)
	runtimeB := testRuntime(c, 11155111, processRegistry)

	router, err := NewRuntimeRouter(runtimeA, runtimeB)

	c.Assert(router, qt.IsNil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "duplicate ProcessIDVersion")
}

func TestRuntimeRouterProcessResolutionErrors(t *testing.T) {
	c := qt.New(t)

	router, err := NewRuntimeRouter(testRuntime(c, 11155111, "0x015eac820688da203a0bd730a8a7a4cdb97e1a02"))
	c.Assert(err, qt.IsNil)

	runtime, err := router.RuntimeForProcess(types.ProcessID{})
	c.Assert(runtime, qt.IsNil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "invalid process ID")

	unknownProcess := types.NewProcessID(
		common.HexToAddress("0x1234567890123456789012345678901234567890"),
		[4]byte{0xde, 0xad, 0xbe, 0xef},
		1,
	)
	c.Assert(router.SupportsProcess(types.ProcessID{}), qt.IsFalse)
	c.Assert(router.SupportsProcess(unknownProcess), qt.IsFalse)
	runtime, err = router.RuntimeForProcess(unknownProcess)
	c.Assert(runtime, qt.IsNil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "runtime not found")

	contracts, err := router.ContractsForProcess(unknownProcess)
	c.Assert(contracts, qt.IsNil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "runtime not found")
}

func testRuntime(c *qt.C, chainID uint64, processRegistry string) *NetworkRuntime {
	contracts := &Contracts{
		ChainID: chainID,
		ContractsAddresses: &Addresses{
			ProcessRegistry: common.HexToAddress(processRegistry),
		},
	}
	runtime, err := NewNetworkRuntime(contracts, nil)
	c.Assert(err, qt.IsNil)
	return runtime
}
