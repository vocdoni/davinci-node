package web3

import (
	"context"
	"fmt"

	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/vocdoni/davinci-node/types"
)

// ProcessChangesFilters returns the list of filters to monitor process changes.
func (c *Contracts) ProcessChangesFilters() []types.Web3FilterFn {
	return []types.Web3FilterFn{
		c.ProcessStatusFilter,
		c.ProcessStateRootFilter,
		c.ProcessMaxVotersFilter,
		c.ProcessCensusRootFilter,
	}
}

// ProcessStatusFilter monitors changes in process status.
func (c *Contracts) ProcessStatusFilter(ctx context.Context, start, end uint64, ch chan<- *types.ProcessWithChanges) error {
	iter, err := c.processes.FilterProcessStatusChanged(&bind.FilterOpts{Start: start, End: &end, Context: ctx}, c.knownPIDs())
	if err != nil || iter == nil {
		return fmt.Errorf("failed to filter status change updated events: %w", err)
	}
	for iter.Next() {
		ch <- &types.ProcessWithChanges{
			ProcessID: iter.Event.ProcessId,
			StatusChange: &types.StatusChange{
				OldStatus: types.ProcessStatus(iter.Event.OldStatus),
				NewStatus: types.ProcessStatus(iter.Event.NewStatus),
			},
		}
	}
	return nil
}

// ProcessStateRootFilter monitors changes in process state root.
func (c *Contracts) ProcessStateRootFilter(ctx context.Context, start, end uint64, ch chan<- *types.ProcessWithChanges) error {
	iter, err := c.processes.FilterProcessStateTransitioned(&bind.FilterOpts{Start: start, End: &end, Context: ctx}, c.knownPIDs(), nil)
	if err != nil || iter == nil {
		return fmt.Errorf("failed to filter state root updated events: %w", err)
	}
	for iter.Next() {
		ch <- &types.ProcessWithChanges{
			ProcessID: iter.Event.ProcessId,
			StateRootChange: &types.StateRootChange{
				OldStateRoot:             new(types.BigInt).SetBigInt(iter.Event.OldStateRoot),
				NewStateRoot:             new(types.BigInt).SetBigInt(iter.Event.NewStateRoot),
				NewVotersCount:           new(types.BigInt).SetBigInt(iter.Event.NewVotersCount),
				NewOverwrittenVotesCount: new(types.BigInt).SetBigInt(iter.Event.NewOverwrittenVotesCount),
				TxHash:                   &iter.Event.Raw.TxHash, // so statesync can fetch the corresponding blob from CL
			},
		}
	}
	return nil
}

// ProcessMaxVotersFilter monitors changes in process max voters.
func (c *Contracts) ProcessMaxVotersFilter(ctx context.Context, start, end uint64, ch chan<- *types.ProcessWithChanges) error {
	iter, err := c.processes.FilterProcessMaxVotersChanged(&bind.FilterOpts{Start: start, End: &end, Context: ctx}, c.knownPIDs())
	if err != nil || iter == nil {
		return fmt.Errorf("failed to filter max voters updated events: %w", err)
	}
	for iter.Next() {
		ch <- &types.ProcessWithChanges{
			ProcessID: iter.Event.ProcessId,
			MaxVotersChange: &types.MaxVotersChange{
				NewMaxVoters: new(types.BigInt).SetBigInt(iter.Event.MaxVoters),
			},
		}
	}
	return nil
}

// ProcessCensusRootFilter monitors changes in process census root.
func (c *Contracts) ProcessCensusRootFilter(ctx context.Context, start, end uint64, ch chan<- *types.ProcessWithChanges) error {
	iter, err := c.processes.FilterCensusUpdated(&bind.FilterOpts{Start: start, End: &end, Context: ctx}, c.knownPIDs())
	if err != nil || iter == nil {
		return fmt.Errorf("failed to filter census updated events: %w", err)
	}
	for iter.Next() {
		ch <- &types.ProcessWithChanges{
			ProcessID: iter.Event.ProcessId,
			CensusRootChange: &types.CensusRootChange{
				NewCensusRoot: iter.Event.CensusRoot[:],
				NewCensusURI:  iter.Event.CensusURI,
			},
		}
	}
	return nil
}
