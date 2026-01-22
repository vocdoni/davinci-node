package ballotproof

import (
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/types"
)

// CircomInputs struct contains the data of the witness to generate a ballot
// proof using the circom circuit.
type CircomInputs struct {
	Fields        []*types.BigInt `json:"fields"`
	BallotMode    *types.BigInt   `json:"packed_ballot_mode"`
	Address       *types.BigInt   `json:"address"`
	Weight        *types.BigInt   `json:"weight"`
	ProcessID     *types.BigInt   `json:"process_id"`
	VoteID        *types.BigInt   `json:"vote_id"`
	EncryptionKey []*types.BigInt `json:"encryption_pubkey"`
	K             *types.BigInt   `json:"k"`
	Cipherfields  []*types.BigInt `json:"cipherfields"`
	InputsHash    *types.BigInt   `json:"inputs_hash"`
}

// BallotProofInputsResult struct contains the result of composing the data to
// generate the witness for a ballot proof using the circom circuit. Includes
// the inputs for the circom circuit but also the required data to cast a vote
// sending it to the sequencer API. It includes the BallotInputsHash, which is
// used by the API to verify the resulting circom proof and the voteID, which
// is signed by the user to prove the ownership of the vote.
type BallotProofInputsResult struct {
	ProcessID        types.ProcessID `json:"processId"`
	Address          types.HexBytes  `json:"address"`
	Weight           *types.BigInt   `json:"weight"`
	Ballot           *elgamal.Ballot `json:"ballot"`
	BallotInputsHash *types.BigInt   `json:"ballotInputsHash"`
	VoteID           types.VoteID    `json:"voteId"`
	CircomInputs     *CircomInputs   `json:"circomInputs"`
}
