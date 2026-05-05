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
