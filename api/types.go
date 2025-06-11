package api

import (
	"github.com/google/uuid"
	"github.com/vocdoni/circom2gnark/parser"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

// NewCensus is the response to a new census creation request.
type NewCensus struct {
	Census uuid.UUID `json:"census"`
}

// CensusRoot is the response to a census root request.
type CensusRoot struct {
	Root types.HexBytes `json:"root"`
}

// CensusParticipant is a participant in a census.
type CensusParticipant struct {
	Key    types.HexBytes `json:"key"`
	Weight *types.BigInt  `json:"weight,omitempty"`
}

// CensusParticipants is a list of participants in a census.
type CensusParticipants struct {
	Participants []*CensusParticipant `json:"participants"`
}

// Vote is the struct to represent a vote in the system. It will be provided by
// the user to cast a vote in a process.
type Vote struct {
	ProcessID        types.HexBytes      `json:"processId"`
	Commitment       *types.BigInt       `json:"commitment"`
	Nullifier        *types.BigInt       `json:"nullifier"`
	CensusProof      types.CensusProof   `json:"censusProof"`
	Ballot           *elgamal.Ballot     `json:"ballot"`
	BallotProof      *parser.CircomProof `json:"ballotProof"`
	BallotInputsHash *types.BigInt       `json:"ballotInputsHash"`
	Address          types.HexBytes      `json:"address"`
	Signature        types.HexBytes      `json:"signature"`
}

// ContractAddresses holds the smart contract addresses needed by the client
type ContractAddresses struct {
	ProcessRegistry           string `json:"process"`
	OrganizationRegistry      string `json:"organization"`
	StateTransitionZKVerifier string `json:"stateTransitionVerifier"`
	ResultsZKVerifier         string `json:"resultsVerifier"`
}

// BallotProofInfo contains all the information needed by a client to generate a ballot zkSNARK proof.
type BallotProofInfo struct {
	CircuitURL           string            `json:"circuitUrl"`
	CircuitHash          string            `json:"circuitHash"`
	WASMhelperURL        string            `json:"ballotProofWasmHelperUrl"`
	WASMhelperHash       string            `json:"ballotProofWasmHelperHash"`
	WASMhelperExecJsURL  string            `json:"ballotProofWasmHelperExecJsUrl"`
	WASMhelperExecJsHash string            `json:"ballotProofWasmHelperExecJsHash"`
	ProvingKeyURL        string            `json:"provingKeyUrl"`
	ProvingKeyHash       string            `json:"provingKeyHash"`
	VerificationKeyURL   string            `json:"verificationKeyUrl"`
	VerificationKeyHash  string            `json:"verificationKeyHash"`
	Contracts            ContractAddresses `json:"contracts"`
}

// VoteResponse is the response returned by the vote submission endpoint.
type VoteResponse struct {
	VoteID types.HexBytes `json:"voteId"`
}

// VoteStatusResponse is the response returned by the vote status endpoint.
type VoteStatusResponse struct {
	Status string `json:"status"`
}

// WorkerJobResponse is the response returned by the worker job submission endpoint.
type WorkerJobResponse struct {
	VoteID       types.HexBytes `json:"voteId"`
	Address      string         `json:"address"`
	SuccessCount int64          `json:"successCount"`
	FailedCount  int64          `json:"failedCount"`
}

// WorkerInfo contains information about a worker node.
type WorkerInfo struct {
	Address      string `json:"address"`
	SuccessCount int64  `json:"successCount"`
	FailedCount  int64  `json:"failedCount"`
}

// WorkersListResponse is the response returned by the workers list endpoint.
type WorkersListResponse struct {
	Workers []WorkerInfo `json:"workers"`
}

// SetMetadataResponse is the response returned by the set metadata endpoint.
type SetMetadataResponse struct {
	Hash types.HexBytes `json:"hash"`
}

// ProcessList is the response returned by the process list endpoint.
type ProcessList struct {
	Processes []types.HexBytes `json:"processes"`
}

// HostLoadResponse is the exact shape we return to the client.
type HostLoadResponse struct {
	MemStats            any                `json:"memStats,omitempty"`
	HostLoad1           float64            `json:"hostLoad1,omitempty"`
	HostMemUsedPercent  float64            `json:"hostMemUsedPercent,omitempty"`
	HostDiskUsedPercent map[string]float64 `json:"hostDiskUsedPercent,omitempty"`
}

// SequencerStatsResponse is the response returned by the sequencer stats endpoint.
type SequencerStatsResponse struct {
	storage.Stats
	ActiveProcesses int `json:"activeProcesses"`
	PendingVotes    int `json:"pendingVotes"`
}
