package types

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

// Web3FilterFn defines the signature for functions that filter process changes
// from the blockchain. These functions take a context, a start and end block
// number, and a channel to send the filtered ProcessWithChanges to. They return
// an error if the filtering fails.
type Web3FilterFn func(ctx context.Context, start, end uint64, ch chan<- *ProcessWithChanges) error

// StatusChange represents a change in the status of a voting process. It
// includes the old and new status values.
type StatusChange struct {
	OldStatus ProcessStatus
	NewStatus ProcessStatus
}

// StateRootChange represents a change in the state root of a voting process.
// It includes the new state root, the updated voters count, and the updated
// count of overwritten votes, as well as the tx hash where the data blob lives,
// that enables a sequencer to reconstruct that NewStateRoot.
type StateRootChange struct {
	OldStateRoot             *BigInt
	NewStateRoot             *BigInt
	NewVotersCount           *BigInt
	NewOverwrittenVotesCount *BigInt
	TxHash                   *common.Hash
}

// MaxVotersChange represents a change in the maximum number of voters
// allowed in a voting process. It includes the new maximum voters value.
type MaxVotersChange struct {
	NewMaxVoters *BigInt
}

// CensusRootChange represents a change in the census root of a voting process.
// It includes the new census root and the associated URI.
type CensusRootChange struct {
	NewCensusRoot HexBytes
	NewCensusURI  string
}

// ProcessWithChanges encapsulates a voting process identifier along with
// various types of changes that may have occurred to the process, such as
// status changes, state root updates, maximum voters adjustments, and census
// root modifications. It includes optional fields for each type of change and
// the process ID.
type ProcessWithChanges struct {
	ProcessID HexBytes
	*StatusChange
	*StateRootChange
	*MaxVotersChange
	*CensusRootChange
}
