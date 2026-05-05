package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	npbindings "github.com/vocdoni/davinci-contracts/golang-types"
)

type web3NetworkConfig struct {
	Network     string   `json:"network"`
	ChainID     uint64   `json:"chainId"`
	RPC         []string `json:"rpc"`
	CAPI        string   `json:"capi"`
	ProcessAddr string   `json:"process"`
}

type web3NetworksConfig []web3NetworkConfig

func (cfg Web3Config) normalizedNetworks() ([]web3NetworkConfig, error) {
	networks := make([]web3NetworkConfig, 0)

	if len(cfg.Networks) > 0 {
		structuredNetworks, err := cfg.Networks.normalized()
		if err != nil {
			return nil, err
		}
		networks = append(networks, structuredNetworks...)
	}

	if cfg.legacyConfigured {
		legacyNetwork, err := normalizeLegacyWeb3Network(cfg)
		if err != nil {
			return nil, err
		}
		networks = append(networks, legacyNetwork)
	}

	if len(networks) == 0 {
		return nil, fmt.Errorf("no web3 network configuration provided")
	}
	if err := validateNormalizedWeb3Networks(networks); err != nil {
		return nil, err
	}
	return networks, nil
}

func (cfg web3NetworksConfig) String() string {
	if len(cfg) == 0 {
		return ""
	}
	raw, err := json.Marshal([]web3NetworkConfig(cfg))
	if err != nil {
		return ""
	}
	return string(raw)
}

func (cfg *web3NetworksConfig) Set(raw string) error {
	networks, err := parseStructuredWeb3NetworksValue(raw)
	if err != nil {
		return err
	}
	*cfg = networks
	return nil
}

func (cfg *web3NetworksConfig) Type() string {
	return "json"
}

func (cfg *web3NetworksConfig) UnmarshalText(text []byte) error {
	return cfg.Set(string(text))
}

func (cfg web3NetworksConfig) normalized() ([]web3NetworkConfig, error) {
	if len(cfg) == 0 {
		return nil, fmt.Errorf("web3.networks cannot be empty")
	}

	normalized := make([]web3NetworkConfig, 0, len(cfg))
	for i, network := range cfg {
		current, err := normalizeConfiguredWeb3Network(network)
		if err != nil {
			return nil, fmt.Errorf("web3.networks[%d]: %w", i, err)
		}
		normalized = append(normalized, current)
	}
	return normalized, nil
}

func parseStructuredWeb3NetworksValue(raw any) (web3NetworksConfig, error) {
	switch value := raw.(type) {
	case nil:
		return nil, nil
	case web3NetworksConfig:
		return value, nil
	case []web3NetworkConfig:
		return web3NetworksConfig(value), nil
	case string:
		if strings.TrimSpace(value) == "" {
			return nil, nil
		}
		return parseStructuredWeb3NetworksJSON(value)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("marshal web3.networks: %w", err)
		}
		return parseStructuredWeb3NetworksJSON(string(encoded))
	}
}

func parseStructuredWeb3NetworksJSON(raw string) (web3NetworksConfig, error) {
	var networks []web3NetworkConfig
	if err := json.Unmarshal([]byte(raw), &networks); err != nil {
		return nil, fmt.Errorf("parse web3.networks JSON: %w", err)
	}
	return web3NetworksConfig(networks), nil
}

func normalizeConfiguredWeb3Network(cfg web3NetworkConfig) (web3NetworkConfig, error) {
	networkName := strings.TrimSpace(cfg.Network)
	processAddr := strings.TrimSpace(cfg.ProcessAddr)
	rpcs := cleanRPCs(cfg.RPC)

	switch {
	case cfg.ChainID == 0 && networkName == "":
		return web3NetworkConfig{}, fmt.Errorf("network or chainId is required")
	case cfg.ChainID == 0:
		chainID, ok := chainIDForNetwork(networkName)
		if !ok {
			return web3NetworkConfig{}, fmt.Errorf("unknown network %q", networkName)
		}
		cfg.ChainID = chainID
	case networkName == "":
		derivedNetwork, ok := networkNameForChainID(cfg.ChainID)
		if !ok {
			return web3NetworkConfig{}, fmt.Errorf("unknown chainId %d", cfg.ChainID)
		}
		networkName = derivedNetwork
	default:
		chainID, ok := chainIDForNetwork(networkName)
		if !ok {
			return web3NetworkConfig{}, fmt.Errorf("unknown network %q", networkName)
		}
		if chainID != cfg.ChainID {
			return web3NetworkConfig{}, fmt.Errorf("network %q does not match chainId %d", networkName, cfg.ChainID)
		}
	}

	if len(rpcs) == 0 {
		return web3NetworkConfig{}, fmt.Errorf("at least one rpc endpoint is required")
	}

	return web3NetworkConfig{
		Network:     networkName,
		ChainID:     cfg.ChainID,
		RPC:         rpcs,
		CAPI:        strings.TrimSpace(cfg.CAPI),
		ProcessAddr: processAddr,
	}, nil
}

func normalizeLegacyWeb3Network(cfg Web3Config) (web3NetworkConfig, error) {
	networkName := strings.TrimSpace(cfg.Network)
	if networkName == "" {
		return web3NetworkConfig{}, fmt.Errorf("legacy web3.network is required")
	}
	chainID, ok := chainIDForNetwork(networkName)
	if !ok {
		return web3NetworkConfig{}, fmt.Errorf("invalid network %s, available networks: %v", cfg.Network, npbindings.AvailableNetworksByName)
	}

	rpcs := cleanRPCs(cfg.Rpc)
	if len(rpcs) == 0 {
		return web3NetworkConfig{}, fmt.Errorf("legacy web3.rpc requires at least one endpoint")
	}

	return web3NetworkConfig{
		Network:     networkName,
		ChainID:     chainID,
		RPC:         rpcs,
		CAPI:        strings.TrimSpace(cfg.Capi),
		ProcessAddr: strings.TrimSpace(cfg.ProcessAddr),
	}, nil
}

func validateNormalizedWeb3Networks(networks []web3NetworkConfig) error {
	seenChainIDs := make(map[uint64]string, len(networks))
	seenNetworks := make(map[string]uint64, len(networks))

	for _, network := range networks {
		if existing, ok := seenChainIDs[network.ChainID]; ok {
			return fmt.Errorf("duplicate chainId %d for networks %q and %q", network.ChainID, existing, network.Network)
		}
		if existing, ok := seenNetworks[network.Network]; ok {
			return fmt.Errorf("duplicate network %q for chainIds %d and %d", network.Network, existing, network.ChainID)
		}
		seenChainIDs[network.ChainID] = network.Network
		seenNetworks[network.Network] = network.ChainID
	}
	return nil
}

func cleanRPCs(rpcs []string) []string {
	cleaned := make([]string, 0, len(rpcs))
	for _, rpc := range rpcs {
		rpc = strings.TrimSpace(rpc)
		if rpc == "" || slices.Contains(cleaned, rpc) {
			continue
		}
		cleaned = append(cleaned, rpc)
	}
	return cleaned
}

func chainIDForNetwork(network string) (uint64, bool) {
	for chainID, name := range npbindings.AvailableNetworksByID {
		if name == network {
			return uint64(chainID), true
		}
	}
	return 0, false
}

func networkNameForChainID(chainID uint64) (string, bool) {
	network, ok := npbindings.AvailableNetworksByID[uint32(chainID)]
	return network, ok
}
