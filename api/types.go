package api

import (
	"github.com/google/uuid"
	"github.com/vocdoni/circom2gnark/parser"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/signatures/ethereum"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
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
// TODO: use directly types.BigInt instead of HexBytes
type Vote struct {
	ProcessID        types.HexBytes          `json:"processId"`
	Commitment       types.HexBytes          `json:"commitment"`
	Nullifier        types.HexBytes          `json:"nullifier"`
	CensusProof      types.CensusProof       `json:"censusProof"`
	Ballot           *elgamal.Ballot         `json:"ballot"`
	BallotProof      *parser.CircomProof     `json:"ballotProof"`
	BallotInputsHash types.HexBytes          `json:"ballotInputsHash"`
	PublicKey        types.HexBytes          `json:"publicKey"`
	Signature        ethereum.ECDSASignature `json:"signature"`
}
