package helpers

import (
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

func CreateOrganization(contracts *web3.Contracts) (common.Address, error) {
	orgAddr := contracts.AccountAddress()
	txHash, err := contracts.CreateOrganization(orgAddr, &types.OrganizationInfo{
		Name:        fmt.Sprintf("Vocdoni test %x", orgAddr[:4]),
		MetadataURI: "https://vocdoni.io",
	})
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to create organization: %w", err)
	}

	if err = contracts.WaitTxByHash(txHash, time.Second*30); err != nil {
		return common.Address{}, fmt.Errorf("failed to wait for organization creation transaction: %w", err)
	}
	return orgAddr, nil
}
