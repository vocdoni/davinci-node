package sequencer

import (
	"errors"
	"fmt"

	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

// currentProcessState retrieves the current in-construction state for a given
// process ID. This state includes all locally processed batches, even if they
// haven't been confirmed on-chain yet. Use this for processing new votes.
func (s *Sequencer) currentProcessState(processID types.ProcessID) (*state.State, error) {
	// get the process from the storage
	process, err := s.stg.Process(processID)
	if err != nil {
		return nil, fmt.Errorf("failed to get process metadata: %w", err)
	}
	isAcceptingVotes, err := s.stg.ProcessIsAcceptingVotes(processID)
	if err != nil {
		return nil, fmt.Errorf("failed to check if process is accepting votes: %w", err)
	}
	if !isAcceptingVotes {
		return nil, fmt.Errorf("process %x is not accepting votes", processID)
	}

	// Open the state tree - this gives us the in-construction root
	st, err := state.New(s.stg.StateDB(), processID)
	if err != nil {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}

	// Initialize if this is the first time
	packedBallotMode, err := process.BallotMode.Pack()
	if err != nil {
		return nil, fmt.Errorf("failed to pack ballot mode: %w", err)
	}
	if err := st.Initialize(
		process.Census.CensusOrigin.BigInt().MathBigInt(),
		packedBallotMode,
		circuits.EncryptionKeyToCircuit(*process.EncryptionKey),
	); err != nil && !errors.Is(err, state.ErrStateAlreadyInitialized) {
		return nil, fmt.Errorf("failed to init state: %w", err)
	}

	// Get the current root from the tree (in-construction state)
	currentRoot, err := st.RootAsBigInt()
	if err != nil {
		return nil, fmt.Errorf("failed to get current root: %w", err)
	}

	log.Debugw("using current in-construction state",
		"processID", processID.String(),
		"currentRoot", currentRoot.String())

	return st, nil
}
