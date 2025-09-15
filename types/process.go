package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/arbo"
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
	ID                   HexBytes              `json:"id,omitempty"             cbor:"0,keyasint,omitempty"`
	Status               ProcessStatus         `json:"status"                   cbor:"1,keyasint,omitempty"`
	OrganizationId       common.Address        `json:"organizationId"           cbor:"2,keyasint,omitempty"`
	EncryptionKey        *EncryptionKey        `json:"encryptionKey"            cbor:"3,keyasint,omitempty"`
	StateRoot            *BigInt               `json:"stateRoot"                cbor:"4,keyasint,omitempty"`
	Result               []*BigInt             `json:"result"                   cbor:"5,keyasint,omitempty"`
	StartTime            time.Time             `json:"startTime"                cbor:"6,keyasint,omitempty"`
	Duration             time.Duration         `json:"duration"                 cbor:"7,keyasint,omitempty"`
	MetadataURI          string                `json:"metadataURI"              cbor:"8,keyasint,omitempty"`
	BallotMode           *BallotMode           `json:"ballotMode"               cbor:"9,keyasint,omitempty"`
	Census               *Census               `json:"census"                   cbor:"10,keyasint,omitempty"`
	Metadata             *Metadata             `json:"metadata,omitempty"       cbor:"11,keyasint,omitempty"`
	VoteCount            *BigInt               `json:"voteCount"                cbor:"12,keyasint,omitempty"`
	VoteOverwrittenCount *BigInt               `json:"voteOverwrittenCount"     cbor:"13,keyasint,omitempty"`
	SequencerStats       SequencerProcessStats `json:"sequencerStats"           cbor:"16,keyasint,omitempty"`
}

// BigCensusRoot returns the BigInt representation of the census root of the
// process. It converts the census root from its original format to a BigInt
// according to the census origin.
func (p *Process) BigCensusRoot() (*BigInt, error) {
	if p.Census == nil {
		return nil, fmt.Errorf("census is nil")
	}
	return processCensusRootToBigInt(p.Census.CensusOrigin, p.Census.CensusRoot)
}

// ProcessWithStatusChange extends types.Process to add OldStatus and NewStatus
// fields
type ProcessWithStatusChange struct {
	*Process
	OldStatus ProcessStatus
	NewStatus ProcessStatus
}

// ProcessWithStateRootChange extends types.Process to add NewStateRoot,
// NewVoteCount, and NewVoteOverwrittenCount fields
type ProcessWithStateRootChange struct {
	*Process
	NewStateRoot            *BigInt
	NewVoteCount            *BigInt
	NewVoteOverwrittenCount *BigInt
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

// CensusOrigin represents the origin of the census used in a voting process.
type CensusOrigin uint8

const (
	CensusOriginUnknown CensusOrigin = iota
	CensusOriginMerkleTree
	CensusOriginCSPEdDSABLS12377

	CensusOriginNameUnknown          = "unknown"
	CensusOriginNameMerkleTree       = "merkle_tree"
	CensusOriginNameCSPEdDSABLS12377 = "csp_eddsa_bls12377"
)

// Valid checks if the CensusOrigin is a valid value.
func (co CensusOrigin) Valid() bool {
	switch co {
	case CensusOriginMerkleTree, CensusOriginCSPEdDSABLS12377:
		return true
	default:
		return false
	}
}

// String returns a string representation of the CensusOrigin.
func (co CensusOrigin) String() string {
	switch co {
	case CensusOriginMerkleTree:
		return CensusOriginNameMerkleTree
	case CensusOriginCSPEdDSABLS12377:
		return CensusOriginNameCSPEdDSABLS12377
	default:
		return CensusOriginNameUnknown
	}
}

// BigInt converts the CensusOrigin to a *types.BigInt representation.
func (co CensusOrigin) BigInt() *BigInt {
	if !co.Valid() {
		return nil
	}
	return (*BigInt)(new(big.Int).SetUint64(uint64(co)))
}

// Number of bytes in the census root
const CensusRootLength = 32

// NormalizedCensusRoot function ensures that the census root is always of a
// fixed length. If its length is not CensusRootLength, it truncates or pads
// it accordingly.
func NormalizedCensusRoot(original HexBytes) HexBytes {
	if len(original) > CensusRootLength {
		// If the original is longer than the allowed length, truncate it
		return original[:CensusRootLength]
	}
	if diff := CensusRootLength - len(original); diff > 0 {
		// If the original is shorter than the allowed length, pad it with
		// zeros at the end
		padded := make(HexBytes, CensusRootLength)
		copy(padded, original)
		return padded
	}
	// If the original is already the correct length, return it as is
	return original
}

type Census struct {
	CensusOrigin CensusOrigin `json:"censusOrigin" cbor:"0,keyasint,omitempty"`
	MaxVotes     *BigInt      `json:"maxVotes"     cbor:"1,keyasint,omitempty"`
	CensusRoot   HexBytes     `json:"censusRoot"   cbor:"2,keyasint,omitempty"`
	CensusURI    string       `json:"censusURI"    cbor:"3,keyasint,omitempty"`
}

// CensusProof is the struct to represent a proof of inclusion in the census
// merkle tree. For example, it will be provided by the user to verify that he
// or she can vote in the process.
type CensusProof struct {
	// Generic fields
	CensusOrigin CensusOrigin `json:"censusOrigin"`
	Root         HexBytes     `json:"root"`
	Address      HexBytes     `json:"address"`
	Weight       *BigInt      `json:"weight,omitempty"`
	// Merkletree related fields
	Siblings HexBytes `json:"siblings,omitempty"`
	Value    HexBytes `json:"value,omitempty"`
	// CSP related fields
	ProcessID HexBytes `json:"processId,omitempty"`
	PublicKey HexBytes `json:"publicKey,omitempty"`
	Signature HexBytes `json:"signature,omitempty"`
}

// CensusRoot represents the census root used in a voting process.
type CensusRoot struct {
	Root HexBytes `json:"root"`
}

// Valid checks that the CensusProof is well-formed
func (cp *CensusProof) Valid() bool {
	if cp == nil {
		return false
	}
	switch cp.CensusOrigin {
	case CensusOriginMerkleTree:
		return cp.Root != nil && cp.Address != nil && cp.Value != nil &&
			cp.Siblings != nil && cp.Weight != nil
	case CensusOriginCSPEdDSABLS12377:
		return cp.Root != nil && cp.Address != nil && cp.ProcessID != nil &&
			cp.PublicKey != nil && cp.Signature != nil
	default:
		return false
	}
}

// HasRoot method checks if the CensusProof has the given census root.
func (cp *CensusProof) HasRoot(censusRoot HexBytes) bool {
	return bytes.Equal(NormalizedCensusRoot(cp.Root), NormalizedCensusRoot(censusRoot))
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
	ProcessID    HexBytes     `json:"processId"`
	CensusOrigin CensusOrigin `json:"censusOrigin"`
	CensusRoot   HexBytes     `json:"censusRoot"`
	BallotMode   *BallotMode  `json:"ballotMode"`
	Signature    HexBytes     `json:"signature"`
}

// CensusRootBigInt returns the BigInt representation of the census root of the
// process. It converts the census root from its original format to a BigInt
// according to the census origin.
func (p *ProcessSetup) CensusRootBigInt() (*BigInt, error) {
	return processCensusRootToBigInt(p.CensusOrigin, p.CensusRoot)
}

// ProcessSetupResponse represents the response of a voting process
type ProcessSetupResponse struct {
	ProcessID        HexBytes    `json:"processId,omitempty"`
	EncryptionPubKey [2]*BigInt  `json:"encryptionPubKey,omitempty"`
	StateRoot        HexBytes    `json:"stateRoot,omitempty"`
	BallotMode       *BallotMode `json:"ballotMode,omitempty"`
}

// processCensusRootToBigInt helper converts the census root from its original
// format to a BigInt according to the census origin.
func processCensusRootToBigInt(origin CensusOrigin, root HexBytes) (*BigInt, error) {
	switch origin {
	case CensusOriginMerkleTree:
		return new(BigInt).SetBigInt(arbo.BytesToBigInt(root)), nil
	case CensusOriginCSPEdDSABLS12377:
		return root.BigInt(), nil
	default:
		return nil, fmt.Errorf("unsupported census origin: %s", origin)
	}
}
