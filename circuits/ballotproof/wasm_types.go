package ballotproof

import (
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

type BallotProofWasmInputs struct {
	Address       types.HexBytes    `json:"address"`
	ProcessID     types.HexBytes    `json:"processID"`
	Secret        types.HexBytes    `json:"secret"`
	EncryptionKey []*types.BigInt   `json:"encryptionKey"`
	K             *types.BigInt     `json:"k"`
	BallotMode    *types.BallotMode `json:"ballotMode"`
	Weight        *types.BigInt     `json:"weight"`
	FieldValues   []*types.BigInt   `json:"fieldValues"`
}

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

type BallotProofWasmResult struct {
	ProccessID       types.HexBytes  `json:"processID"`
	Address          types.HexBytes  `json:"address"`
	Commitment       *types.BigInt   `json:"commitment"`
	Nullifier        *types.BigInt   `json:"nullifier"`
	Ballot           *elgamal.Ballot `json:"ballot"`
	BallotInputsHash types.HexBytes  `json:"ballotInputsHash"`
	CircomInputs     *CircomInputs   `json:"circomInputs"`
}
