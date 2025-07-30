package storage

import (
	"math/big"
	"time"

	"github.com/vocdoni/davinci-node/types"
)

// Common update functions for use with UpdateProcess

// ProcessUpdateCallbackStateRoot returns a function that updates the state
// root and vote counts
func ProcessUpdateCallbackStateRoot(root *types.BigInt, numNewVotes, numOverwritten *big.Int) func(*types.Process) error {
	return func(p *types.Process) error {
		p.StateRoot = root
		if numNewVotes != nil {
			p.VoteCount = new(types.BigInt).Add(p.VoteCount, (*types.BigInt)(numNewVotes))
		}
		if numOverwritten != nil {
			p.VoteOverwrittenCount = new(types.BigInt).Add(p.VoteOverwrittenCount, (*types.BigInt)(numOverwritten))
		}
		return nil
	}
}

// ProcessUpdateCallbackFinalization returns a function that marks a process
// as finalized with results
func ProcessUpdateCallbackFinalization(results []*types.BigInt) func(*types.Process) error {
	return func(p *types.Process) error {
		p.Status = types.ProcessStatusResults
		p.Result = results
		return nil
	}
}

// ProcessUpdateCallbackSetStatus returns a function that updates the process
// status. This function is used to set the status of a process, such as ready,
// ended, canceled, etc.
func ProcessUpdateCallbackSetStatus(status types.ProcessStatus) func(*types.Process) error {
	return func(p *types.Process) error {
		p.Status = status
		return nil
	}
}

// ProcessUpdateCallbackSetStateRoot returns a function that updates the state
// root and vote counts of a process. This function is used when a update over
// the state root is received from the process monitor.
func ProcessUpdateCallbackSetStateRoot(newRoot *types.BigInt, newCount, newOverwrittenCount *types.BigInt) func(*types.Process) error {
	return func(p *types.Process) error {
		// Update the process only if the state root is different.
		if !p.StateRoot.Equal(newRoot) {
			p.VoteCount = newCount
			p.StateRoot = newRoot
			// If the overwritten count is greater than the current one,
			// update it as well.
			if p.VoteOverwrittenCount.LessThan(newOverwrittenCount) {
				p.VoteOverwrittenCount = newOverwrittenCount
			}
		}
		return nil
	}
}

// ProcessUpdateCallbackLastTransitionDate returns a function that updates the
// last state transition date
func ProcessUpdateCallbackLastTransitionDate() func(*types.Process) error {
	return func(p *types.Process) error {
		p.SequencerStats.LastStateTransitionDate = time.Now()
		return nil
	}
}
