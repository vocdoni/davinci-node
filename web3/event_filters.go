package web3

import (
	"context"
	"fmt"

	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// ProcessUpdatesFilters returns the list of filters to monitor process changes.
func (c *Contracts) ProcessUpdatesFilters() []types.Web3FilterFn {
	return []types.Web3FilterFn{
		c.NewProcessFilter,
		c.ProcessStatusFilter,
		c.ProcessStateRootFilter,
		c.ProcessMaxVotersFilter,
		c.ProcessCensusRootFilter,
	}
}

// NewProcessFilter monitors the creation of new processes.
func (c *Contracts) NewProcessFilter(ctx context.Context, start, end uint64, ch chan<- *types.ProcessWithChanges) error {
	iter, err := c.processes.FilterProcessCreated(&bind.FilterOpts{Start: start, End: &end, Context: ctx}, nil, nil)
	if err != nil || iter == nil {
		return fmt.Errorf("failed to filter process created events: %w", err)
	}
	for iter.Next() {
		// Get the process info from the contract
		process, err := c.Process(iter.Event.ProcessId)
		if err != nil {
			log.Warnw("error getting new process info",
				"processID", types.HexBytes(iter.Event.ProcessId[:]).String(),
				"error", err.Error())
			continue
		}
		// Try to add the process ID to the known list. If it already exists, skip.
		if c.RegisterUnknownProcess(iter.Event.ProcessId) {
			continue
		}
		// Emit the new process event
		ch <- &types.ProcessWithChanges{
			ProcessID: iter.Event.ProcessId,
			NewProcess: &types.NewProcess{
				Process: process,
			},
		}
	}
	return nil
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
