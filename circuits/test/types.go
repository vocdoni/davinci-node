package circuitstest

import (
	"math/big"

	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

// AggregatorTestResults struct includes relevant data after AggregatorCircuit inputs generation
type AggregatorTestResults struct {
	InputsHash *big.Int
	Process    circuits.Process[*big.Int]
	Votes      []state.Vote
}

// VoteVerifierTestResults struct includes relevant data after VerifyVoteCircuit inputs generation
type VoteVerifierTestResults struct {
	InputsHashes     []*big.Int
	EncryptionPubKey circuits.EncryptionKey[*big.Int]
	Addresses        []*big.Int
	ProcessID        *big.Int
	CensusOrigin     types.CensusOrigin
	CensusRoot       *big.Int
	Ballots          []elgamal.Ballot
	VoteIDs          []types.HexBytes
}
