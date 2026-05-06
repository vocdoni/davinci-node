package rpc

import (
	"context"
	"fmt"
	"slices"
)

// GroupEndpointsByChainID groups the provided RPC endpoints by the chain ID
// they report over JSON-RPC.
func GroupEndpointsByChainID(rpcs []string) (map[uint64][]string, error) {
	if len(rpcs) == 0 {
		return nil, fmt.Errorf("no web3 endpoints provided")
	}

	pool := NewWeb3Pool()
	grouped := make(map[uint64][]string)
	var lastErr error
	for _, uri := range rpcs {
		chainID, err := pool.AddEndpoint(uri)
		if err != nil {
			lastErr = err
			continue
		}
		grouped[chainID] = append(grouped[chainID], uri)
	}
	if len(grouped) == 0 {
		if lastErr != nil {
			return nil, fmt.Errorf("no usable web3 endpoints provided: %w", lastErr)
		}
		return nil, fmt.Errorf("no usable web3 endpoints provided")
	}
	return grouped, nil
}

// EndpointsForChainID returns the endpoints that report the expected chain ID
// and rejects mixed-chain endpoint sets.
func EndpointsForChainID(rpcs []string, chainID uint64) ([]string, error) {
	grouped, err := GroupEndpointsByChainID(rpcs)
	if err != nil {
		return nil, err
	}

	matching := grouped[chainID]
	if len(matching) == 0 {
		return nil, fmt.Errorf("no web3 endpoints matched chain ID %d", chainID)
	}

	unexpectedChainIDs := make([]uint64, 0, len(grouped))
	for discoveredChainID := range grouped {
		if discoveredChainID != chainID {
			unexpectedChainIDs = append(unexpectedChainIDs, discoveredChainID)
		}
	}
	if len(unexpectedChainIDs) > 0 {
		slices.Sort(unexpectedChainIDs)
		return nil, fmt.Errorf("web3 endpoints include unexpected chain IDs %v for expected chain ID %d", unexpectedChainIDs, chainID)
	}

	return matching, nil
}

// GroupBeaconEndpointsByChainID groups the provided beacon endpoints by the
// chain ID
func GroupBeaconEndpointsByChainID(ctx context.Context, beaconEndpoints []string) (map[uint64][]string, error) {
	mapChainIDs := make(map[uint64][]string)
	for _, endpoint := range beaconEndpoints {
		chainID, err := BeaconChainID(ctx, endpoint)
		if err != nil {
			return nil, fmt.Errorf("resolve chain ID for endpoint %s: %w", endpoint, err)
		}
		if _, ok := mapChainIDs[chainID]; !ok {
			mapChainIDs[chainID] = []string{}
		}
		mapChainIDs[chainID] = append(mapChainIDs[chainID], endpoint)
	}
	return mapChainIDs, nil
}
