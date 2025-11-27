package types

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/crypto/ecc"
)

type ProcessStatus uint8

const (
	ProcessStatusReady    = ProcessStatus(iota) // Process is ready to be started
	ProcessStatusEnded                          // Process has ended and waiting for results
	ProcessStatusCanceled                       // Process has been canceled
	ProcessStatusPaused                         // Process is paused
	ProcessStatusResults                        // Process has results available

	ProcessStatusReadyName    = "ready"
	ProcessStatusEndedName    = "ended"
	ProcessStatusCanceledName = "canceled"
	ProcessStatusPausedName   = "paused"
	ProcessStatusResultsName  = "results"

	// NewProcessMessageToSign is the message to sign when creating a new voting process
	NewProcessMessageToSign = "I am creating a new voting process for the davinci.vote protocol identified with id %s"
)

func (s ProcessStatus) String() string {
	switch s {
	case ProcessStatusReady:
		return ProcessStatusReadyName
	case ProcessStatusEnded:
		return ProcessStatusEndedName
	case ProcessStatusCanceled:
		return ProcessStatusCanceledName
	case ProcessStatusPaused:
		return ProcessStatusPausedName
	case ProcessStatusResults:
		return ProcessStatusResultsName
	default:
		return "unknown"
	}
}

type (
	GenericMetadata    map[string]any
	MultilingualString map[string]string
)

// MarshalJSON implements json.Marshaler interface for GenericMetadata
// Returns an empty object {} instead of null when the map is nil or empty
func (g GenericMetadata) MarshalJSON() ([]byte, error) {
	if g == nil {
		return []byte("{}"), nil
	}
	// Normalize nested maps to plain map[string]any
	normalized := normalizeMaps(map[string]any(g))
	return json.Marshal(normalized)
}

func normalizeMaps(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case GenericMetadata:
			out[k] = normalizeMaps(map[string]any(val))
		case map[string]any:
			out[k] = normalizeMaps(val)
		default:
			out[k] = v
		}
	}
	return out
}

func (g *GenericMetadata) UnmarshalJSON(data []byte) error {
	if g == nil {
		return fmt.Errorf("GenericMetadata: nil receiver")
	}

	// First unmarshal into a plain map
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*g = convertToGenericMetadata(raw)
	return nil
}

func convertToGenericMetadata(m map[string]interface{}) GenericMetadata {
	out := make(GenericMetadata, len(m))
	for k, v := range m {
		switch vv := v.(type) {
		case map[string]any:
			out[k] = convertToGenericMetadata(vv)
		default:
			out[k] = v
		}
	}
	return out
}

// MarshalJSON implements json.Marshaler interface for MultilingualString
// Returns an empty object {} instead of null when the map is nil or empty
func (m MultilingualString) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	// Use the default map marshaling behavior
	return json.Marshal(map[string]string(m))
}

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
	Meta        GenericMetadata    `json:"meta" cbor:"6,keyasint,omitempty"`
}

func (m *Metadata) String() string {
	data, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(data)
}

type Process struct {
	ID                    *ProcessID            `json:"id,omitempty"             cbor:"0,keyasint,omitempty"`
	Status                ProcessStatus         `json:"status"                   cbor:"1,keyasint,omitempty"`
	OrganizationId        common.Address        `json:"organizationId"           cbor:"2,keyasint,omitempty"`
	EncryptionKey         *EncryptionKey        `json:"encryptionKey"            cbor:"3,keyasint,omitempty"`
	StateRoot             *BigInt               `json:"stateRoot"                cbor:"4,keyasint,omitempty"`
	Result                []*BigInt             `json:"result"                   cbor:"5,keyasint,omitempty"`
	StartTime             time.Time             `json:"startTime"                cbor:"6,keyasint,omitempty"`
	Duration              time.Duration         `json:"duration"                 cbor:"7,keyasint,omitempty"`
	MetadataURI           string                `json:"metadataURI"              cbor:"8,keyasint,omitempty"`
	BallotMode            *BallotMode           `json:"ballotMode"               cbor:"9,keyasint,omitempty"`
	Census                *Census               `json:"census"                   cbor:"10,keyasint,omitempty"`
	Metadata              *Metadata             `json:"metadata,omitempty"       cbor:"11,keyasint,omitempty"`
	VotersCount           *BigInt               `json:"votersCount"        cbor:"12,keyasint,omitempty"`
	OverwrittenVotesCount *BigInt               `json:"overwrittenVotesCount"    cbor:"13,keyasint,omitempty"`
	MaxVoters             *BigInt               `json:"maxVoters"                cbor:"14,keyasint,omitempty"`
	SequencerStats        SequencerProcessStats `json:"sequencerStats"           cbor:"16,keyasint,omitempty"`
}

type SequencerProcessStats struct {
	StateTransitionCount        int       `json:"stateTransitionCount" cbor:"0,keyasint,omitempty"`
	LastStateTransitionDate     time.Time `json:"lastStateTransitionDate" cbor:"1,keyasint,omitempty"`
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
	TypeStatsLastTransitionDate
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

// EncryptionKeyFromPoint returns an EncryptionKey with the X and Y coordinates of the elliptic curve element.
func EncryptionKeyFromPoint(publicKey ecc.Point) EncryptionKey {
	x, y := publicKey.Point()
	return EncryptionKey{
		X: BigIntConverter(x),
		Y: BigIntConverter(y),
	}
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
	ProcessID  ProcessID   `json:"processId"`
	Census     *Census     `json:"census"`
	BallotMode *BallotMode `json:"ballotMode"`
	Signature  HexBytes    `json:"signature"`
}

// ProcessSetupResponse represents the response of a voting process
type ProcessSetupResponse struct {
	ProcessID        *ProcessID  `json:"processId,omitempty"`
	EncryptionPubKey [2]*BigInt  `json:"encryptionPubKey,omitempty"`
	StateRoot        HexBytes    `json:"stateRoot,omitempty"`
	BallotMode       *BallotMode `json:"ballotMode,omitempty"`
}
