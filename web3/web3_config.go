package web3

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/web3/rpc"
	"github.com/vocdoni/davinci-node/web3/txmanager"
)

// Web3Config holds Ethereum-related configuration for the sequencer
type Web3Config struct {
	PrivKey                 string   `mapstructure:"privkey"`                 // Private key for the Ethereum account
	ChainIDs                []uint   `mapstructure:"chainIDs"`                // Chain IDs to use, if defined, limits RPCs and BeaconAPIs, if empty, use all
	RPCs                    []string `mapstructure:"rpc"`                     // Web3 RPC endpoints, can be multiple
	BeaconAPIs              []string `mapstructure:"bapi"`                    // Web3 Consensus Beacon API endpoints, can be multiple
	GasMultiplier           float64  `mapstructure:"gasMultiplier"`           // Gas price multiplier for transactions (default: 1.0)
	ProcessRegistryContract string   `mapstructure:"processRegistryContract"` // Process registry smart contract reference (<chainID>:<address>)
}

func (web3Cfg Web3Config) InitRuntimes(ctx context.Context) ([]*NetworkRuntime, error) {
	// Group RPC endpoints by chain ID
	rpcsMap, err := rpc.GroupEndpointsByChainID(web3Cfg.RPCs)
	if err != nil {
		return nil, fmt.Errorf("resolve RPC endpoints: %w", err)
	}
	// Group beacon API endpoints by chain ID
	beaconAPIsMap, err := rpc.GroupBeaconEndpointsByChainID(ctx, web3Cfg.BeaconAPIs)
	if err != nil {
		return nil, fmt.Errorf("resolve beacon API endpoints: %w", err)
	}
	// Check if any network is configured and RPCs should be limited
	limitedNetworks := len(web3Cfg.ChainIDs) > 0
	// Iterate over available networks to initialize web3 runtimes
	var runtimes []*NetworkRuntime
	for chainID, rpcs := range rpcsMap {
		// Skip networks that are not configured, if any is configured
		if limitedNetworks && !slices.Contains(web3Cfg.ChainIDs, uint(chainID)) {
			continue
		}
		// Try to find a beacon API for this network
		var beaconAPI string
		if beaconAPIs, ok := beaconAPIsMap[chainID]; ok && len(beaconAPIs) > 0 {
			beaconAPI = beaconAPIs[0]
		}
		// Try to initialize web3 runtime
		addresses := web3Cfg.addressesByChainID(chainID)
		runtime, err := initializeNetworkRuntime(ctx, addresses, rpcs, beaconAPI, web3Cfg.PrivKey, web3Cfg.GasMultiplier)
		if err != nil {
			return nil, fmt.Errorf("initialize web3 runtime for chain ID %d: %w", chainID, err)
		}
		runtimes = append(runtimes, runtime)
	}
	return runtimes, nil
}

// addressesByChainID returns the address of the process registry contract for
// the given chain ID. By default, it returns nil, which means that the default
// addresses should be used. If a process registry contract is specified in the
// config, it will be used if the chain ID matches.
func (web3Cfg Web3Config) addressesByChainID(chainID uint64) *Addresses {
	// If no process registry contract is specified, return nil
	if web3Cfg.ProcessRegistryContract == "" {
		return nil
	}
	// Ensure that the contract definition is in the expected format:
	//    <chainID>:<address>
	parts := strings.Split(web3Cfg.ProcessRegistryContract, ":")
	if len(parts) != 2 {
		return nil
	}
	// Parse the chain ID from the contract prefix
	parsedChainID, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || parsedChainID != chainID {
		return nil
	}
	// Parse the address from the contract suffix
	if addr := common.HexToAddress(parts[1]); addr != (common.Address{}) {
		return &Addresses{ProcessRegistry: addr}
	}
	return nil
}

func initializeNetworkRuntime(
	ctx context.Context,
	addresses *Addresses,
	rpcEndpoints []string,
	beaconAPIEndpoint string,
	privKey string,
	gasMultiplier float64,
) (*NetworkRuntime, error) {
	// Load contracts for this network
	contracts, err := New(rpcEndpoints, beaconAPIEndpoint, gasMultiplier)
	if err != nil {
		return nil, fmt.Errorf("initialize web3 client: %w", err)
	}

	if err := contracts.LoadContracts(addresses); err != nil {
		return nil, fmt.Errorf("initialize contracts: %w", err)
	}

	if err := contracts.SetAccountPrivateKey(privKey); err != nil {
		return nil, fmt.Errorf("set account private key: %w", err)
	}

	txManager, err := txmanager.New(
		ctx,
		contracts.Web3Pool(),
		contracts.Client(),
		contracts.Signer(),
		txmanager.DefaultConfig(contracts.ChainID),
	)
	if err != nil {
		return nil, fmt.Errorf("create transaction manager: %w", err)
	}
	txManager.Start(ctx)
	contracts.SetTxManager(txManager)

	runtime, err := NewNetworkRuntime(contracts, txManager)
	if err != nil {
		txManager.Stop()
		return nil, fmt.Errorf("create runtime: %w", err)
	}
	return runtime, nil
}
