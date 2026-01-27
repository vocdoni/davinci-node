package web3

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	npbindings "github.com/vocdoni/davinci-contracts/golang-types"
	"github.com/vocdoni/davinci-node/types"
)

// FetchOnchainCensusRoot retrieves the census root from the specified census
// validator contract address.
func (c *Contracts) FetchOnchainCensusRoot(address common.Address) (types.HexBytes, error) {
	// Ensure the address is valid
	if address == (common.Address{}) {
		return nil, fmt.Errorf("invalid contract address")
	}
	// Instance the census validator contract bindings
	censusValidator, err := npbindings.NewICensusValidator(address, c.cli)
	if err != nil {
		return nil, fmt.Errorf("failed to bind census validator contract: %w", err)
	}
	// Fetch the census root from the contract
	bigRoot, err := censusValidator.GetCensusRoot(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch census root from contract: %w", err)
	}
	// Convert to HexBytes and return
	return bigRoot.Bytes(), nil
}
