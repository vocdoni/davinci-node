package sequencer

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

const (
	errGetProcessMetadata         = "failed to get process metadata: %w"
	errCheckProcessAcceptingVotes = "failed to check if process is accepting votes: %w"
)

// currentProcessState retrieves the current in-construction state for a given
// process ID. This state includes all locally processed batches, even if they
// haven't been confirmed on-chain yet. Use this for processing new votes.
func (s *Sequencer) currentProcessState(processID types.ProcessID) (*state.State, error) {
	// get the process from the storage
	process, err := s.stg.Process(processID)
	if err != nil {
		return nil, fmt.Errorf(errGetProcessMetadata, err)
	}
	isAcceptingVotes, err := s.stg.ProcessIsAcceptingVotes(processID)
	if err != nil {
		return nil, fmt.Errorf(errCheckProcessAcceptingVotes, err)
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
		*process.EncryptionKey,
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

// filterBallotsByCensus returns only the ballots whose voter address is
// present in the process census tree. Ballots with absent addresses are
// discarded and logged at WARN level. If the census tree is unavailable
// (no root), an error is returned so the caller can retry rather than
// silently discarding all ballots.
//
// For CSP-based censuses no local merkle lookup is possible; all ballots
// are returned unchanged.
func (s *Sequencer) filterBallotsByCensus(processID types.ProcessID, ballots []*storage.AggregatorBallot) ([]*storage.AggregatorBallot, error) {
	process, err := s.stg.Process(processID)
	if err != nil {
		return nil, fmt.Errorf(errGetProcessMetadata, err)
	}
	if !process.Census.CensusOrigin.IsMerkleTree() {
		// CSP censuses are verified via the embedded proof; no local tree to query.
		return ballots, nil
	}

	var chainID uint64
	if process.Census.CensusOrigin == types.CensusOriginMerkleTreeOnchainDynamicV1 {
		contracts, err := s.contractsForProcess(processID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve contracts for process %s: %w", processID.String(), err)
		}
		chainID = contracts.ChainID
	}
	censusRef, err := s.stg.LoadCensus(chainID, process.Census)
	if err != nil {
		return nil, fmt.Errorf("failed to load census for process %s: %w", processID.String(), err)
	}
	censusTree := censusRef.Tree()
	if _, ok := censusTree.Root(); !ok {
		return nil, fmt.Errorf("census tree has no root for process %s (censusRoot=%s)",
			processID.String(), process.Census.CensusRoot.String())
	}

	filtered := make([]*storage.AggregatorBallot, 0, len(ballots))
	for _, b := range ballots {
		addr := common.BigToAddress(b.Address)
		if _, ok := censusTree.GetWeight(addr); !ok {
			log.Warnw("address not found in census, skipping ballot",
				"processID", processID.String(),
				"address", addr.Hex(),
			)
			continue
		}
		filtered = append(filtered, b)
	}
	return filtered, nil
}
