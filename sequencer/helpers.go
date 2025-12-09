package sequencer

import (
	"errors"
	"fmt"

	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

// latestProcessState retrieves and initializes the latest state for a given
// process ID. It ensures that the local state is synchronized with the
// on-chain state.
func (s *Sequencer) latestProcessState(pid *types.ProcessID) (*state.State, error) {
	// get the process from the storage
	process, err := s.stg.Process(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to get process metadata: %w", err)
	}
	isAcceptingVotes, err := s.stg.ProcessIsAcceptingVotes(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to check if process is accepting votes: %w", err)
	}
	if !isAcceptingVotes {
		return nil, fmt.Errorf("process %x is not accepting votes", pid)
	}

	st, err := state.New(s.stg.StateDB(), pid.BigInt())
	if err != nil {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}

	if err := st.Initialize(
		process.Census.CensusOrigin.BigInt().MathBigInt(),
		circuits.BallotModeToCircuit(process.BallotMode),
		circuits.EncryptionKeyToCircuit(*process.EncryptionKey),
	); err != nil && !errors.Is(err, state.ErrStateAlreadyInitialized) {
		return nil, fmt.Errorf("failed to init state: %w", err)
	}

	// get the on-chain state root to ensure we are in sync
	onchainStateRoot, err := s.contracts.StateRoot(pid.Marshal())
	if err != nil {
		return nil, fmt.Errorf("failed to get on-chain state root: %w", err)
	}

	// if the on-chain state root is different from the local one, update it
	if onchainStateRoot.MathBigInt().Cmp(process.StateRoot.MathBigInt()) != 0 {
		if err := st.RootExists(onchainStateRoot.MathBigInt()); err != nil {
			return nil, fmt.Errorf("on-chain state root does not exist in local state: %w", err)
		}
		if err := s.stg.UpdateProcess(pid, storage.ProcessUpdateCallbackSetStateRoot(onchainStateRoot, nil, nil, nil)); err != nil {
			return nil, fmt.Errorf("failed to update process state root: %w", err)
		}
		log.Warnw("local state root mismatch, updated local state root to match on-chain",
			"pid", pid.String(),
			"local", process.StateRoot.String(),
			"onchain", onchainStateRoot.String(),
		)
	}

	// initialize the process state on the given root
	processState, err := state.LoadOnRoot(s.stg.StateDB(), pid.BigInt(), onchainStateRoot.MathBigInt())
	if err != nil {
		return nil, fmt.Errorf("failed to create state: %w", err)
	}
	return processState, nil
}
