package main

import (
	"testing"

	qt "github.com/frankban/quicktest"
)

func TestNormalizedNetworksFromLegacyConfig(t *testing.T) {
	c := qt.New(t)

	cfg := Web3Config{
		Network:          "sepolia",
		Rpc:              []string{"https://rpc.sepolia.example"},
		Capi:             "https://beacon.sepolia.example",
		ProcessAddr:      "0x1234",
		legacyConfigured: true,
	}

	networks, err := cfg.normalizedNetworks()

	c.Assert(err, qt.IsNil)
	c.Assert(networks, qt.HasLen, 1)
	c.Assert(networks[0], qt.DeepEquals, web3NetworkConfig{
		Network:     "sepolia",
		ChainID:     11155111,
		RPC:         []string{"https://rpc.sepolia.example"},
		CAPI:        "https://beacon.sepolia.example",
		ProcessAddr: "0x1234",
	})
}

func TestNormalizedNetworksFromLegacyConfigWithoutRPC(t *testing.T) {
	c := qt.New(t)

	cfg := Web3Config{
		Network:          "sepolia",
		legacyConfigured: true,
	}

	networks, err := cfg.normalizedNetworks()

	c.Assert(err, qt.IsNil)
	c.Assert(networks, qt.HasLen, 1)
	c.Assert(networks[0], qt.DeepEquals, web3NetworkConfig{
		Network: "sepolia",
		ChainID: 11155111,
		RPC:     []string{},
	})
}

func TestNormalizedNetworksFromStructuredConfig(t *testing.T) {
	c := qt.New(t)

	cfg := Web3Config{
		Networks: web3NetworksConfig{
			{
				Network: "sepolia",
				ChainID: 11155111,
				RPC:     []string{"https://rpc.sepolia.example"},
				CAPI:    "https://beacon.sepolia.example",
			},
			{
				ChainID:     42220,
				RPC:         []string{"https://rpc.celo.example"},
				ProcessAddr: "0xabcd",
			},
		},
	}

	networks, err := cfg.normalizedNetworks()

	c.Assert(err, qt.IsNil)
	c.Assert(networks, qt.DeepEquals, []web3NetworkConfig{
		{
			Network: "sepolia",
			ChainID: 11155111,
			RPC:     []string{"https://rpc.sepolia.example"},
			CAPI:    "https://beacon.sepolia.example",
		},
		{
			Network:     "celo",
			ChainID:     42220,
			RPC:         []string{"https://rpc.celo.example"},
			ProcessAddr: "0xabcd",
		},
	})
}

func TestNormalizedNetworksFromStructuredConfigWithoutRPC(t *testing.T) {
	c := qt.New(t)

	cfg := Web3Config{
		Networks: web3NetworksConfig{
			{
				Network: "sepolia",
				ChainID: 11155111,
			},
		},
	}

	networks, err := cfg.normalizedNetworks()

	c.Assert(err, qt.IsNil)
	c.Assert(networks, qt.DeepEquals, []web3NetworkConfig{
		{
			Network: "sepolia",
			ChainID: 11155111,
			RPC:     []string{},
		},
	})
}

func TestNormalizedNetworksMergesStructuredAndExplicitLegacyConfig(t *testing.T) {
	c := qt.New(t)

	cfg := Web3Config{
		Network:          "arbitrum",
		Rpc:              []string{"https://rpc.arbitrum.example"},
		legacyConfigured: true,
		Networks: web3NetworksConfig{
			{
				Network: "sepolia",
				ChainID: 11155111,
				RPC:     []string{"https://rpc.sepolia.example"},
			},
		},
	}

	networks, err := cfg.normalizedNetworks()

	c.Assert(err, qt.IsNil)
	c.Assert(networks, qt.DeepEquals, []web3NetworkConfig{
		{
			Network: "sepolia",
			ChainID: 11155111,
			RPC:     []string{"https://rpc.sepolia.example"},
		},
		{
			Network: "arbitrum",
			ChainID: 42161,
			RPC:     []string{"https://rpc.arbitrum.example"},
		},
	})
}

func TestValidateNormalizedNetworksAllowsSameChainID(t *testing.T) {
	c := qt.New(t)

	// Same chainID with different process registries is valid — the runtime
	// router distinguishes them via ProcessIDVersion (chainID || registry addr).
	networks := []web3NetworkConfig{
		{Network: "sepolia-v1", ChainID: 11155111, ProcessAddr: "0xold"},
		{Network: "sepolia-v2", ChainID: 11155111, ProcessAddr: "0xnew"},
	}

	err := validateNormalizedWeb3Networks(networks)
	c.Assert(err, qt.IsNil)
}

func TestNormalizedNetworksRejectsDuplicateNetworkName(t *testing.T) {
	c := qt.New(t)

	cfg := Web3Config{
		Networks: web3NetworksConfig{
			{
				Network: "sepolia",
				ChainID: 11155111,
				RPC:     []string{"https://rpc.sepolia.example"},
			},
		},
		Network:          "sepolia",
		Rpc:              []string{"https://rpc.sepolia-legacy.example"},
		legacyConfigured: true,
	}

	networks, err := cfg.normalizedNetworks()

	c.Assert(networks, qt.IsNil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "duplicate network")
}

func TestNormalizedNetworksRejectsMismatchedStructuredNetwork(t *testing.T) {
	c := qt.New(t)

	cfg := Web3Config{
		Networks: web3NetworksConfig{
			{
				Network: "sepolia",
				ChainID: 42220,
				RPC:     []string{"https://rpc.example"},
			},
		},
	}

	networks, err := cfg.normalizedNetworks()

	c.Assert(networks, qt.IsNil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, `network "sepolia" does not match chainId 42220`)
}

func TestParseStructuredWeb3NetworksValueFromJSON(t *testing.T) {
	c := qt.New(t)

	networks, err := parseStructuredWeb3NetworksValue(`[{"network":"sepolia","chainId":11155111,"rpc":["https://rpc.sepolia.example"]}]`)

	c.Assert(err, qt.IsNil)
	c.Assert(networks, qt.DeepEquals, web3NetworksConfig{
		{
			Network: "sepolia",
			ChainID: 11155111,
			RPC:     []string{"https://rpc.sepolia.example"},
		},
	})
}

func TestWeb3NetworksConfigStringRoundTrip(t *testing.T) {
	c := qt.New(t)

	original := web3NetworksConfig{
		{
			Network: "sepolia",
			ChainID: 11155111,
			RPC:     []string{"https://rpc.sepolia.example"},
		},
	}

	var decoded web3NetworksConfig
	err := decoded.Set(original.String())

	c.Assert(err, qt.IsNil)
	c.Assert(decoded, qt.DeepEquals, original)
}

func TestShouldIncludeLegacyWeb3Network(t *testing.T) {
	testCases := []struct {
		name                 string
		hasStructuredNetwork bool
		legacyNetworkSet     bool
		wantConfigured       bool
	}{
		{
			name:                 "single network mode always includes legacy config",
			hasStructuredNetwork: false,
			legacyNetworkSet:     false,
			wantConfigured:       true,
		},
		{
			name:                 "structured networks without legacy network",
			hasStructuredNetwork: true,
			legacyNetworkSet:     false,
			wantConfigured:       false,
		},
		{
			name:                 "structured networks with explicit legacy network",
			hasStructuredNetwork: true,
			legacyNetworkSet:     true,
			wantConfigured:       true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := qt.New(t)

			configured, err := shouldIncludeLegacyWeb3Network(
				tc.hasStructuredNetwork,
				tc.legacyNetworkSet,
			)

			c.Assert(err, qt.IsNil)
			c.Assert(configured, qt.Equals, tc.wantConfigured)
		})
	}
}
