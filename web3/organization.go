package web3

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// CreateOrganization creates a new organization in the OrganizationRegistry contract.
func (c *Contracts) CreateOrganization(address common.Address, orgInfo *types.OrganizationInfo) (common.Hash, error) {
	txOpts, err := c.authTransactOpts()
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to create transact options: %w", err)
	}
	tx, err := c.organizations.CreateOrganization(txOpts, address, orgInfo.Name, orgInfo.MetadataURI, []common.Address{c.signer.Address()})
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to create organization: %w", err)
	}
	return tx.Hash(), nil
}

// Organization returns the organization with the given address from the OrganizationRegistry contract.
func (c *Contracts) Organization(address common.Address) (*types.OrganizationInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	name, uri, err := c.organizations.GetOrganization(&bind.CallOpts{Context: ctx}, address)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("failed to get organization: %w", err)
	}
	return &types.OrganizationInfo{
		Name:        name,
		MetadataURI: uri,
	}, nil
}

// MonitorOrganizationCreatedByPolling monitors the creation of organizations by polling the logs of the blockchain.
func (c *Contracts) MonitorOrganizationCreatedByPolling(ctx context.Context, interval time.Duration) (<-chan *types.OrganizationInfo, error) {
	ch := make(chan *types.OrganizationInfo)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Warnw("exiting monitor organizations creation")
				return
			case <-ticker.C:
				ctxQuery, cancel := context.WithTimeout(ctx, web3QueryTimeout)
				iter, err := c.organizations.FilterOrganizationCreated(&bind.FilterOpts{Start: c.lastWatchOrgBlock, Context: ctxQuery}, nil, nil)
				cancel()
				if err != nil || iter == nil {
					log.Warnw("failed to filter organization created, retrying", "err", err)
					continue
				}
				for iter.Next() {
					id := fmt.Sprintf("%x", iter.Event.Id)
					if _, exists := c.knownOrganizations[id]; exists {
						continue
					}
					c.knownOrganizations[id] = struct{}{}
					org, err := c.Organization(iter.Event.Id)
					if err != nil {
						log.Errorw(err, "failed to get organization while monitoring")
						continue
					}
					org.ID = iter.Event.Id
					c.lastWatchOrgBlock = iter.Event.Raw.BlockNumber
					ch <- org
				}
			}
		}
	}()
	return ch, nil
}
