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

func TestNormalizedNetworksRejectsDuplicateChainID(t *testing.T) {
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
	c.Assert(err.Error(), qt.Contains, "duplicate chainId 11155111")
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
		legacyConfigured     bool
		legacyNetworkSet     bool
		wantConfigured       bool
		wantErr              string
	}{
		{
			name:                 "single network mode always includes legacy config",
			hasStructuredNetwork: false,
			legacyConfigured:     false,
			legacyNetworkSet:     false,
			wantConfigured:       true,
		},
		{
			name:                 "structured networks without legacy flags",
			hasStructuredNetwork: true,
			legacyConfigured:     false,
			legacyNetworkSet:     false,
			wantConfigured:       false,
		},
		{
			name:                 "mixed mode requires explicit legacy network",
			hasStructuredNetwork: true,
			legacyConfigured:     true,
			legacyNetworkSet:     false,
			wantErr:              "web3.network must be explicitly set",
		},
		{
			name:                 "mixed mode accepts explicit legacy network",
			hasStructuredNetwork: true,
			legacyConfigured:     true,
			legacyNetworkSet:     true,
			wantConfigured:       true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := qt.New(t)

			configured, err := shouldIncludeLegacyWeb3Network(
				tc.hasStructuredNetwork,
				tc.legacyConfigured,
				tc.legacyNetworkSet,
			)

			if tc.wantErr != "" {
				c.Assert(err, qt.Not(qt.IsNil))
				c.Assert(err.Error(), qt.Contains, tc.wantErr)
				return
			}

			c.Assert(err, qt.IsNil)
			c.Assert(configured, qt.Equals, tc.wantConfigured)
		})
	}
}
