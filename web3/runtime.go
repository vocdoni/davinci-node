package web3

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3/txmanager"
)

// Since ProcessIDVersion is a uint32, we limit the chain ID to 32 bits
const maxProcessIDChainID = uint64(^uint32(0))

// NetworkRuntime groups all chain-specific runtime state needed by the
// sequencer for one enabled network.
type NetworkRuntime struct {
	Network          string
	ProcessIDVersion [4]byte
	Contracts        *Contracts
	TxManager        *txmanager.TxManager
}

// NewNetworkRuntime builds a network runtime and computes its ProcessIDVersion
// from the contracts chain ID and ProcessRegistry address.
func NewNetworkRuntime(
	network string,
	contracts *Contracts,
	txManager *txmanager.TxManager,
) (*NetworkRuntime, error) {
	if network == "" {
		return nil, fmt.Errorf("network is required")
	}
	if contracts == nil {
		return nil, fmt.Errorf("contracts is required")
	}
	if contracts.ContractsAddresses == nil {
		return nil, fmt.Errorf("contracts addresses are required")
	}
	processRegistry := contracts.ContractsAddresses.ProcessRegistry
	if processRegistry == (common.Address{}) {
		return nil, fmt.Errorf("process registry address is required")
	}
	// Check that the chain ID is within the ProcessIDVersion limit
	if contracts.ChainID > maxProcessIDChainID {
		return nil, fmt.Errorf("chain ID %d exceeds ProcessIDVersion limit", contracts.ChainID)
	}

	return &NetworkRuntime{
		Network:          network,
		ProcessIDVersion: types.ProcessIDVersion(uint32(contracts.ChainID), processRegistry),
		Contracts:        contracts,
		TxManager:        txManager,
	}, nil
}

// RuntimeRouter resolves process-scoped operations to the correct runtime.
type RuntimeRouter struct {
	runtimes         []*NetworkRuntime
	runtimeByVersion map[[4]byte]*NetworkRuntime
}

// NewRuntimeRouter creates a router and validates that each runtime has a
// unique ProcessIDVersion.
func NewRuntimeRouter(runtimes ...*NetworkRuntime) (*RuntimeRouter, error) {
	router := &RuntimeRouter{
		runtimes:         make([]*NetworkRuntime, 0, len(runtimes)),
		runtimeByVersion: make(map[[4]byte]*NetworkRuntime, len(runtimes)),
	}

	for _, runtime := range runtimes {
		if runtime == nil {
			return nil, fmt.Errorf("runtime cannot be nil")
		}
		if runtime.Contracts == nil {
			return nil, fmt.Errorf("runtime contracts cannot be nil")
		}
		if existing, ok := router.runtimeByVersion[runtime.ProcessIDVersion]; ok {
			return nil, fmt.Errorf(
				"duplicate ProcessIDVersion %x for networks %s and %s",
				runtime.ProcessIDVersion,
				existing.Network,
				runtime.Network,
			)
		}
		router.runtimes = append(router.runtimes, runtime)
		router.runtimeByVersion[runtime.ProcessIDVersion] = runtime
	}

	return router, nil
}

// RuntimeForVersion returns the runtime associated with the provided
// ProcessIDVersion.
func (r *RuntimeRouter) RuntimeForVersion(version [4]byte) (*NetworkRuntime, bool) {
	if r == nil {
		return nil, false
	}
	runtime, ok := r.runtimeByVersion[version]
	return runtime, ok
}

// ContractsForProcess resolves the contracts instance associated with the
// provided process ID.
func (r *RuntimeRouter) ContractsForProcess(processID types.ProcessID) (*Contracts, error) {
	if !processID.IsValid() {
		return nil, fmt.Errorf("invalid process ID")
	}
	runtime, ok := r.RuntimeForVersion(processID.Version())
	if !ok {
		return nil, fmt.Errorf("runtime not found for process version %x", processID.Version())
	}
	return runtime.Contracts, nil
}

// Runtimes returns the configured runtimes in registration order.
func (r *RuntimeRouter) Runtimes() []*NetworkRuntime {
	if r == nil {
		return nil
	}
	return append([]*NetworkRuntime(nil), r.runtimes...)
}
