package storage

import (
	"time"

	"github.com/vocdoni/davinci-node/types"
)

// Common update functions for use with UpdateProcess

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
// root and voters counts of a process. This function is used when a update over
// the state root is received from the process monitor.
func ProcessUpdateCallbackSetStateRoot(stateRoot, maxVoters, votersCount, overwrittenVotesCount *types.BigInt) func(*types.Process) error {
	return func(p *types.Process) error {
		if p.StateRoot == nil {
			p.StateRoot = stateRoot
		}
		// Update max voters if provided
		if maxVoters != nil {
			p.MaxVoters = maxVoters
		}
		// Update the process only if the state root is different.
		if !p.StateRoot.Equal(stateRoot) {
			p.StateRoot = stateRoot
			if votersCount != nil {
				p.VotersCount = votersCount
			}
			if overwrittenVotesCount != nil {
				p.OverwrittenVotesCount = overwrittenVotesCount
			}
		}
		return nil
	}
}

// ProcessUpdateCallbackSetMaxVoters returns a function that updates the max
// voters of a process.
func ProcessUpdateCallbackSetMaxVoters(maxVoters *types.BigInt) func(*types.Process) error {
	return func(p *types.Process) error {
		if maxVoters != nil {
			p.MaxVoters = maxVoters
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
