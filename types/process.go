package types

import (
	"encoding/json"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
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
	ProcessType ProcessType        `json:"processType" cbor:"4,keyasint,omitempty"`
}

type Process struct {
	ID             HexBytes       `json:"id,omitempty"             cbor:"0,keyasint,omitempty"`
	Status         uint8          `json:"status"                   cbor:"1,keyasint,omitempty"`
	OrganizationId common.Address `json:"organizationId"           cbor:"2,keyasint,omitempty"`
	EncryptionKey  *EncryptionKey `json:"encryptionKey"            cbor:"3,keyasint,omitempty"`
	StateRoot      HexBytes       `json:"stateRoot"                cbor:"4,keyasint,omitempty"`
	Result         []*BigInt      `json:"result"                   cbor:"5,keyasint,omitempty"`
	StartTime      time.Time      `json:"startTime"                cbor:"6,keyasint,omitempty"`
	Duration       time.Duration  `json:"duration"                 cbor:"7,keyasint,omitempty"`
	MetadataURI    string         `json:"metadataURI"              cbor:"8,keyasint,omitempty"`
	BallotMode     *BallotMode    `json:"ballotMode"               cbor:"9,keyasint,omitempty"`
	Census         *Census        `json:"census"                   cbor:"10,keyasint,omitempty"`
	Metadata       *Metadata      `json:"metadata,omitempty"       cbor:"11,keyasint,omitempty"`
}

func (p *Process) String() string {
	data, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(data)
}

type EncryptionKey struct {
	X *big.Int `json:"x" cbor:"0,keyasint,omitempty"`
	Y *big.Int `json:"y" cbor:"1,keyasint,omitempty"`
}

type Census struct {
	CensusOrigin uint8    `json:"censusOrigin" cbor:"0,keyasint,omitempty"`
	MaxVotes     *BigInt  `json:"maxVotes"     cbor:"1,keyasint,omitempty"`
	CensusRoot   HexBytes `json:"censusRoot"   cbor:"2,keyasint,omitempty"`
	CensusURI    string   `json:"censusURI"    cbor:"3,keyasint,omitempty"`
}

// CensusProof holds the result of a Merkle proof.
type CensusProof struct {
	Root     HexBytes `json:"root"`
	Key      HexBytes `json:"key"`
	Value    HexBytes `json:"value"`
	Siblings HexBytes `json:"siblings"`
	Weight   *BigInt  `json:"weight"`
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
	CensusRoot HexBytes   `json:"censusRoot"`
	BallotMode BallotMode `json:"ballotRules"`
	Nonce      uint64     `json:"nonce"`
	ChainID    uint32     `json:"chainId"`
	Signature  []byte     `json:"signature"`
}

// ProcessSetupResponse represents the response of a voting process
type ProcessSetupResponse struct {
	ProcessID        HexBytes  `json:"processId"`
	EncryptionPubKey [2]BigInt `json:"encryptionPubKey,omitempty"`
	StateRoot        HexBytes  `json:"stateRoot,omitempty"`
	ChainID          uint32    `json:"chainId,omitempty"`
	Nonce            uint64    `json:"nonce,omitempty"`
	Address          string    `json:"address,omitempty"`
}
