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

	tests := []struct {
		desc       string
		config     string
		chainID    uint64
		wantAddr   common.Address
		wantResult bool // true if we expect a non-nil *Addresses result
	}{
		{
			desc:       "empty config returns nil",
			config:     "",
			chainID:    11155111,
			wantResult: false,
		},
		{
			desc:       "valid chainID:address match",
			config:     "11155111:0x015eac820688da203a0bd730a8a7a4cdb97e1a02",
			chainID:    11155111,
			wantAddr:   sepoliaAddr,
			wantResult: true,
		},
		{
			desc:       "valid chainID:address for anvil",
			config:     "31337:0xe7f1725e7734ce288f8367e1bb143e90bb3f0512",
			chainID:    31337,
			wantAddr:   anvilAddr,
			wantResult: true,
		},
		{
			desc:       "chain ID mismatch returns nil",
			config:     "11155111:0x015eac820688da203a0bd730a8a7a4cdb97e1a02",
			chainID:    1,
			wantResult: false,
		},
		{
			desc:       "address only (no chainID prefix) returns nil",
			config:     "0x015eac820688da203a0bd730a8a7a4cdb97e1a02",
			chainID:    11155111,
			wantResult: false,
		},
		{
			desc:       "non-numeric chain ID returns nil",
			config:     "sepolia:0x015eac820688da203a0bd730a8a7a4cdb97e1a02",
			chainID:    11155111,
			wantResult: false,
		},
		{
			desc:       "zero address returns nil",
			config:     "11155111:0x0000000000000000000000000000000000000000",
			chainID:    11155111,
			wantResult: false,
		},
		{
			desc:       "empty address part returns nil",
			config:     "11155111:",
			chainID:    11155111,
			wantResult: false,
		},
	}

	for _, tt := range tests {
		c.Run(tt.desc, func(c *qt.C) {
			cfg := Web3Config{ProcessRegistryContract: tt.config}
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
