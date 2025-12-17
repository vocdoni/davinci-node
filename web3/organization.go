package web3

import (
	"context"
	"fmt"

	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/types"
)

// CreateOrganization creates a new organization in the OrganizationRegistry
// contract.
func (c *Contracts) CreateOrganization(address common.Address, orgInfo *types.OrganizationInfo) (common.Hash, error) {
	// Fallback to old method if transaction manager not initialized
	txOpts, err := c.authTransactOpts()
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to create transact options: %w", err)
	}
	tx, err := c.organizations.CreateOrganization(txOpts, orgInfo.Name, orgInfo.MetadataURI, []common.Address{c.signer.Address()})
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to create organization: %w", err)
	}
	return tx.Hash(), nil
}

// Organization returns the organization with the given address from the
// OrganizationRegistry contract.
func (c *Contracts) Organization(address common.Address) (*types.OrganizationInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	org, err := c.organizations.GetOrganization(&bind.CallOpts{Context: ctx}, address)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("failed to get organization: %w", err)
	}
	return &types.OrganizationInfo{
		Name:        org.Name,
		MetadataURI: org.MetadataURI,
	}, nil
}
