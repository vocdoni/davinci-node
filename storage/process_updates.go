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
		if status != types.ProcessStatusReady {
			p.IsAcceptingVotes = false // If the process is not ready, it should not accept votes
		}
		return nil
	}
}

// ProcessUpdateCallbackSetStateRoot returns a function that updates the state
// root and vote counts of a process. This function is used when a update over
// the state root is received from the process monitor.
func ProcessUpdateCallbackSetStateRoot(newRoot *types.BigInt, newCount, newOverwrittenCount *types.BigInt) func(*types.Process) error {
	// By the moment, do not update the state root, just the vote counts.
	// If the state root is updated, the sequencer should request the updated
	// state tree from other sequencers.
	return func(p *types.Process) error {
		// Update the process only if the new vote count are greater than the
		// current ones and the state root is different.
		if p.VoteCount.LessThan(newCount) && !p.StateRoot.Equal(newRoot) {
			p.VoteCount = newCount
			p.StateRoot = newRoot
			// If the overwritten count is greater than the current one,
			// update it as well.
			if p.VoteOverwrittenCount.LessThan(newOverwrittenCount) {
				p.VoteOverwrittenCount = newOverwrittenCount
			}
			// Currently, if the state root is updated by external sequencers,
			// the current sequencer cannot operate on the process any more. So
			// we need to avoid errors trying to work with it, mark it as
			// inactive in the sequencer and do not accept votes.
			p.IsLocallyActive = false
			p.IsAcceptingVotes = false
		}
		return nil
	}
}

// ProcessUpdateCallbackAcceptingVotes returns a function that updates the
// accepting votes flag
func ProcessUpdateCallbackAcceptingVotes(accepting bool) func(*types.Process) error {
	return func(p *types.Process) error {
		p.IsAcceptingVotes = accepting
		return nil
	}
}

// ProcessUpdateCallbackActiveLocally returns a function that updates the local
// activity status of the process
func ProcessUpdateCallbackActiveLocally(active bool) func(*types.Process) error {
	return func(p *types.Process) error {
		p.IsLocallyActive = active
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
