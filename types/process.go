package types

import (
	"encoding/json"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

const (
	ProcessStatusReady    = uint8(iota) // Process is ready to be started
	ProcessStatusEnded                  // Process has ended and waiting for results
	ProcessStatusCanceled               // Process has been canceled
	ProcessStatusPaused                 // Process is paused
	ProcessStatusResults                // Process has results available
)

type (
	GenericMetadata    map[string]string
	MultilingualString map[string]string
)

type MediaMetadata struct {
	Header string `json:"header" cbor:"0,keyasint,omitempty"`
	Logo   string `json:"logo"   cbor:"1,keyasint,omitempty"`
}

type Choice struct {
	Title MultilingualString `json:"title" cbor:"0,keyasint,omitempty"`
	Value int                `json:"value" cbor:"1,keyasint,omitempty"`
	Meta  GenericMetadata    `json:"meta"  cbor:"2,keyasint,omitempty"`
}

type Question struct {
	Title       MultilingualString `json:"title"       cbor:"0,keyasint,omitempty"`
	Description MultilingualString `json:"description" cbor:"1,keyasint,omitempty"`
	Choices     []Choice           `json:"choices"     cbor:"2,keyasint,omitempty"`
	Meta        GenericMetadata    `json:"meta"        cbor:"3,keyasint,omitempty"`
}

type ProcessType struct {
	Name       string          `json:"name"       cbor:"0,keyasint,omitempty"`
	Properties GenericMetadata `json:"properties" cbor:"1,keyasint,omitempty"`
}

type Metadata struct {
	Title       MultilingualString `json:"title"       cbor:"0,keyasint,omitempty"`
	Description MultilingualString `json:"description" cbor:"1,keyasint,omitempty"`
	Media       MediaMetadata      `json:"media"       cbor:"2,keyasint,omitempty"`
	Questions   []Question         `json:"questions"   cbor:"3,keyasint,omitempty"`
	Type        ProcessType        `json:"type" cbor:"4,keyasint,omitempty"`
	Version     string             `json:"version" cbor:"5,keyasint,omitempty"`
	Meta        GenericMetadata    `json:"meta,omitempty" cbor:"6,keyasint,omitempty"`
}

func (m *Metadata) String() string {
	data, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(data)
}

type Process struct {
	ID                 HexBytes              `json:"id,omitempty"             cbor:"0,keyasint,omitempty"`
	Status             uint8                 `json:"status"                   cbor:"1,keyasint,omitempty"`
	OrganizationId     common.Address        `json:"organizationId"           cbor:"2,keyasint,omitempty"`
	EncryptionKey      *EncryptionKey        `json:"encryptionKey"            cbor:"3,keyasint,omitempty"`
	StateRoot          *BigInt               `json:"stateRoot"                cbor:"4,keyasint,omitempty"`
	Result             []*BigInt             `json:"result"                   cbor:"5,keyasint,omitempty"`
	StartTime          time.Time             `json:"startTime"                cbor:"6,keyasint,omitempty"`
	Duration           time.Duration         `json:"duration"                 cbor:"7,keyasint,omitempty"`
	MetadataURI        string                `json:"metadataURI"              cbor:"8,keyasint,omitempty"`
	BallotMode         *BallotMode           `json:"ballotMode"               cbor:"9,keyasint,omitempty"`
	Census             *Census               `json:"census"                   cbor:"10,keyasint,omitempty"`
	Metadata           *Metadata             `json:"metadata,omitempty"       cbor:"11,keyasint,omitempty"`
	VoteCount          *BigInt               `json:"voteCount"                cbor:"12,keyasint,omitempty"`
	VoteOverwriteCount *BigInt               `json:"voteOverwriteCount"       cbor:"13,keyasint,omitempty"`
	IsFinalized        bool                  `json:"isFinalized"              cbor:"14,keyasint,omitempty"`
	IsAcceptingVotes   bool                  `json:"isAcceptingVotes"         cbor:"15,keyasint,omitempty"`
	SequencerStats     SequencerProcessStats `json:"sequencerStats"           cbor:"16,keyasint"`
}

type SequencerProcessStats struct {
	StateTransitionCount        int       `json:"stateTransitionCount" cbor:"0,keyasint,omitempty"`
	LasStateTransitionDate      time.Time `json:"lastStateTransitionDate" cbor:"1,keyasint,omitempty"`
	SettledStateTransitionCount int       `json:"settledStateTransitionCount" cbor:"2,keyasint,omitempty"`
	AggregatedVotesCount        int       `json:"aggregatedVotesCount" cbor:"3,keyasint,omitempty"`
	VerifiedVotesCount          int       `json:"verifiedVotesCount"   cbor:"4,keyasint,omitempty"`
	PendingVotesCount           int       `json:"pendingVotesCount"    cbor:"5,keyasint,omitempty"`
	CurrentBatchSize            int       `json:"currentBatchSize"     cbor:"6,keyasint,omitempty"`
	LastBatchSize               int       `json:"lastBatchSize"        cbor:"7,keyasint,omitempty"`
}

// TypeStats are used to identify the type of stats in the Process
type TypeStats int

const (
	TypeStatsStateTransitions TypeStats = iota
	TypeStatsSettledStateTransitions
	TypeStatsAggregatedVotes
	TypeStatsVerifiedVotes
	TypeStatsPendingVotes
	TypeStatsCurrentBatchSize
	TypeStatsLastBatchSize
)

func (p *Process) String() string {
	data, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(data)
}

type EncryptionKey struct {
	X *BigInt `json:"x" cbor:"0,keyasint,omitempty"`
	Y *BigInt `json:"y" cbor:"1,keyasint,omitempty"`
}

type Census struct {
	CensusOrigin uint8    `json:"censusOrigin" cbor:"0,keyasint,omitempty"`
	MaxVotes     *BigInt  `json:"maxVotes"     cbor:"1,keyasint,omitempty"`
	CensusRoot   HexBytes `json:"censusRoot"   cbor:"2,keyasint,omitempty"`
	CensusURI    string   `json:"censusURI"    cbor:"3,keyasint,omitempty"`
}

// CensusProof is the struct to represent a proof of inclusion in the census
// merkle tree. For example, it will be provided by the user to verify that he
// or she can vote in the process.
type CensusProof struct {
	Root     HexBytes `json:"root"`
	Key      HexBytes `json:"key"`
	Value    HexBytes `json:"value"`
	Siblings HexBytes `json:"siblings"`
	Weight   *BigInt  `json:"weight"`
}

// Valid checks that the CensusProof is well-formed
func (cp *CensusProof) Valid() bool {
	return cp.Root != nil && cp.Key != nil && cp.Value != nil &&
		cp.Siblings != nil && cp.Weight != nil
}

// String returns a string representation of the CensusProof
// in JSON format. It returns an empty string if the JSON marshaling fails.
func (cp *CensusProof) String() string {
	data, err := json.Marshal(cp)
	if err != nil {
		return ""
	}
	return string(data)
}

type OrganizationInfo struct {
	ID          common.Address `json:"id,omitempty"      cbor:"0,keyasint,omitempty"`
	Name        string         `json:"name"              cbor:"1,keyasint,omitempty"`
	MetadataURI string         `json:"metadataURI"       cbor:"2,keyasint,omitempty"`
}

func (o *OrganizationInfo) String() string {
	data, err := json.Marshal(o)
	if err != nil {
		return ""
	}
	return string(data)
}

// ProcessSetup is the struct to create a new voting process
type ProcessSetup struct {
	CensusRoot HexBytes    `json:"censusRoot"`
	BallotMode *BallotMode `json:"ballotMode"`
	Nonce      uint64      `json:"nonce"`
	ChainID    uint32      `json:"chainId"`
	Signature  HexBytes    `json:"signature"`
}

// ProcessSetupResponse represents the response of a voting process
type ProcessSetupResponse struct {
	ProcessID        HexBytes    `json:"processId"`
	EncryptionPubKey [2]*BigInt  `json:"encryptionPubKey,omitempty"`
	StateRoot        HexBytes    `json:"stateRoot,omitempty"`
	ChainID          uint32      `json:"chainId,omitempty"`
	Nonce            uint64      `json:"nonce,omitempty"`
	Address          string      `json:"address,omitempty"`
	BallotMode       *BallotMode `json:"ballotMode,omitempty"`
}
