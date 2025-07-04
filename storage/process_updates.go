package storage

import (
	"math/big"
	"time"

	"github.com/vocdoni/davinci-node/types"
)

// Common update functions for use with UpdateProcess

// ProcessUpdateCallbackStateRoot returns a function that updates the state root and vote counts
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

// ProcessUpdateCallbackFinalization returns a function that marks a process as finalized with results
func ProcessUpdateCallbackFinalization(results []*types.BigInt) func(*types.Process) error {
	return func(p *types.Process) error {
		p.Status = types.ProcessStatusResults
		p.Result = results
		return nil
	}
}

// ProcessUpdateCallbackSetStatus returns a function that updates the process status
// This function is used to set the status of a process, such as ready, ended, canceled, etc.
func ProcessUpdateCallbackSetStatus(status types.ProcessStatus) func(*types.Process) error {
	return func(p *types.Process) error {
		p.Status = status
		if status != types.ProcessStatusReady {
			p.IsAcceptingVotes = false // If the process is not ready, it should not accept votes
		}
		return nil
	}
}

// ProcessUpdateCallbackAcceptingVotes returns a function that updates the accepting votes flag
func ProcessUpdateCallbackAcceptingVotes(accepting bool) func(*types.Process) error {
	return func(p *types.Process) error {
		p.IsAcceptingVotes = accepting
		return nil
	}
}

// ProcessUpdateCallbackLastTransitionDate returns a function that updates the last state transition date
func ProcessUpdateCallbackLastTransitionDate() func(*types.Process) error {
	return func(p *types.Process) error {
		p.SequencerStats.LastStateTransitionDate = time.Now()
		return nil
	}
}
