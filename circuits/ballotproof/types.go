package ballotproof

import (
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// BallotProofInputs struct contains the required inputs to compose the
// data to generate the witness for a ballot proof using the circom circuit.
type BallotProofInputs struct {
	Address       types.HexBytes    `json:"address"`
	ProcessID     types.HexBytes    `json:"processID"`
	Secret        types.HexBytes    `json:"secret"`
	EncryptionKey []*types.BigInt   `json:"encryptionKey"`
	K             *types.BigInt     `json:"k"`
	BallotMode    *types.BallotMode `json:"ballotMode"`
	Weight        *types.BigInt     `json:"weight"`
	FieldValues   []*types.BigInt   `json:"fieldValues"`
}

// CircomInputs struct contains the data of the witness to generate a ballot
// proof using the circom circuit.
type CircomInputs struct {
	Fields          []string `json:"fields"`
	MaxCount        string   `json:"max_count"`
	ForceUniqueness string   `json:"force_uniqueness"`
	MaxValue        string   `json:"max_value"`
	MinValue        string   `json:"min_value"`
	MaxTotalCost    string   `json:"max_total_cost"`
	MinTotalCost    string   `json:"min_total_cost"`
	CostExp         string   `json:"cost_exp"`
	CostFromWeight  string   `json:"cost_from_weight"`
	Address         string   `json:"address"`
	Weight          string   `json:"weight"`
	ProcessID       string   `json:"process_id"`
	PK              []string `json:"pk"`
	K               string   `json:"k"`
	Cipherfields    []string `json:"cipherfields"`
	Nullifier       string   `json:"nullifier"`
	Commitment      string   `json:"commitment"`
	Secret          string   `json:"secret"`
	InputsHash      string   `json:"inputs_hash"`
}

// BallotProofInputsResult struct contains the result of composing the data to
// generate the witness for a ballot proof using the circom circuit. Includes
// the inputs for the circom circuit but also the required data to cast a vote
// sending it to the sequencer API. It includes the BallotInputsHash, which is
// used by the API to verify the resulting circom proof and the voteID, which
// is signed by the user to prove the ownership of the vote.
type BallotProofInputsResult struct {
	ProccessID       types.HexBytes  `json:"processID"`
	Address          types.HexBytes  `json:"address"`
	Commitment       *types.BigInt   `json:"commitment"`
	Nullifier        *types.BigInt   `json:"nullifier"`
	Ballot           *elgamal.Ballot `json:"ballot"`
	BallotInputsHash *types.BigInt   `json:"ballotInputHash"`
	VoteID           types.HexBytes  `json:"voteID"`
	CircomInputs     *CircomInputs   `json:"circomInputs"`
}
