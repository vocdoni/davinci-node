package web3

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
)

func TestAddressesByChainID(t *testing.T) {
	c := qt.New(t)

	sepoliaAddr := common.HexToAddress("0x015eac820688da203a0bd730a8a7a4cdb97e1a02")
	anvilAddr := common.HexToAddress("0xe7f1725e7734ce288f8367e1bb143e90bb3f0512")
	mainnetAddr := common.HexToAddress("0x9b7c0b5e1240373c2d8a1f3b7e0b2d8d4a6f3c2e")

	tests := []struct {
		desc       string
		contracts  []string // ProcessRegistryContract entries
		chainID    uint64
		wantAddr   common.Address
		wantResult bool // true if we expect a non-nil *Addresses result
	}{
		{
			desc:       "empty config returns nil",
			contracts:  nil,
			chainID:    11155111,
			wantResult: false,
		},
		{
			desc:       "single valid match",
			contracts:  []string{"11155111:0x015eac820688da203a0bd730a8a7a4cdb97e1a02"},
			chainID:    11155111,
			wantAddr:   sepoliaAddr,
			wantResult: true,
		},
		{
			desc:       "single valid match for anvil",
			contracts:  []string{"31337:0xe7f1725e7734ce288f8367e1bb143e90bb3f0512"},
			chainID:    31337,
			wantAddr:   anvilAddr,
			wantResult: true,
		},
		{
			desc:       "chain ID mismatch returns nil",
			contracts:  []string{"11155111:0x015eac820688da203a0bd730a8a7a4cdb97e1a02"},
			chainID:    1,
			wantResult: false,
		},
		{
			desc:       "address only (no chainID prefix) skips entry",
			contracts:  []string{"0x015eac820688da203a0bd730a8a7a4cdb97e1a02"},
			chainID:    11155111,
			wantResult: false,
		},
		{
			desc:       "non-numeric chain ID skips entry",
			contracts:  []string{"sepolia:0x015eac820688da203a0bd730a8a7a4cdb97e1a02"},
			chainID:    11155111,
			wantResult: false,
		},
		{
			desc:       "zero address skips entry",
			contracts:  []string{"11155111:0x0000000000000000000000000000000000000000"},
			chainID:    11155111,
			wantResult: false,
		},
		{
			desc:       "empty address part skips entry",
			contracts:  []string{"11155111:"},
			chainID:    11155111,
			wantResult: false,
		},
		{
			desc: "multiple entries, first matching returns",
			contracts: []string{
				"11155111:0x015eac820688da203a0bd730a8a7a4cdb97e1a02",
				"42220:0x68dac70af68aa0bed8cef36c523243941d7d7876",
			},
			chainID:    11155111,
			wantAddr:   sepoliaAddr,
			wantResult: true,
		},
		{
			desc: "multiple entries, later entry matches",
			contracts: []string{
				"1:0x9b7c0b5e1240373c2d8a1f3b7e0b2d8d4a6f3c2e",
				"11155111:0x015eac820688da203a0bd730a8a7a4cdb97e1a02",
			},
			chainID:    11155111,
			wantAddr:   sepoliaAddr,
			wantResult: true,
		},
		{
			desc: "multiple entries, none match returns nil",
			contracts: []string{
				"42220:0x68dac70af68aa0bed8cef36c523243941d7d7876",
				"1:0x9b7c0b5e1240373c2d8a1f3b7e0b2d8d4a6f3c2e",
			},
			chainID:    11155111,
			wantResult: false,
		},
		{
			desc: "invalid entries skipped, valid later entry matches",
			contracts: []string{
				"bad",
				":",
				"11155111:",
				"11155111:0x0000000000000000000000000000000000000000",
				"11155111:0x015eac820688da203a0bd730a8a7a4cdb97e1a02",
			},
			chainID:    11155111,
			wantAddr:   sepoliaAddr,
			wantResult: true,
		},
		{
			desc: "all entries invalid returns nil",
			contracts: []string{
				"bad",
				"nope:0x015eac820688da203a0bd730a8a7a4cdb97e1a02",
				"11155111:",
			},
			chainID:    11155111,
			wantResult: false,
		},
		{
			desc: "matching entry with mainnet address",
			contracts: []string{
				"11155111:0x015eac820688da203a0bd730a8a7a4cdb97e1a02",
				"1:0x9b7c0b5e1240373c2d8a1f3b7e0b2d8d4a6f3c2e",
			},
			chainID:    1,
			wantAddr:   mainnetAddr,
			wantResult: true,
		},
	}

	for _, tt := range tests {
		c.Run(tt.desc, func(c *qt.C) {
			cfg := Web3Config{ProcessRegistryContract: tt.contracts}
			result := cfg.addressesByChainID(tt.chainID)

			if !tt.wantResult {
				c.Assert(result, qt.IsNil)
				return
			}
			c.Assert(result, qt.Not(qt.IsNil))
			c.Assert(result.ProcessRegistry, qt.DeepEquals, tt.wantAddr)
		})
	}
}
