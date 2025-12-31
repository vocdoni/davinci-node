package types

import (
	"encoding/json"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/twistededwards"
)

// CensusOrigin represents the origin of the census used in a voting process.
type CensusOrigin uint8

const (
	CensusOriginUnknown CensusOrigin = iota
	CensusOriginMerkleTreeOffchainStaticV1
	CensusOriginMerkleTreeOffchainDynamicV1
	CensusOriginMerkleTreeOnchainV1
	CensusOriginCSPEdDSABN254V1

	CensusOriginNameUnknown                     = "unknown"
	CensusOriginNameMerkleTreeOffchainStaticV1  = "merkle_tree_offchain_static_v1"
	CensusOriginNameMerkleTreeOffchainDynamicV1 = "merkle_tree_offchain_dynamic_v1"
	CensusOriginNameMerkleTreeOnchainV1         = "merkle_tree_onchain_v1"
	CensusOriginNameCSPEdDSABN254V1             = "csp_eddsa_bn254_v1"

	// CensusRootLength defines the length in bytes of the census root.
	CensusRootLength = 32
)

var supportedCensusOrigins = map[CensusOrigin]string{
	CensusOriginMerkleTreeOffchainStaticV1:  CensusOriginNameMerkleTreeOffchainStaticV1,
	CensusOriginMerkleTreeOffchainDynamicV1: CensusOriginNameMerkleTreeOffchainDynamicV1,
	// TODO: bring back when implemented
	// CensusOriginMerkleTreeOnchainV1: CensusOriginNameMerkleTreeOnchainV1,
	CensusOriginCSPEdDSABN254V1: CensusOriginNameCSPEdDSABN254V1,
}

// CurveID returns the twistededwards.ID associated with the CensusOrigin. Only
// CSP origins have an associated curve, the rest return UNKNOWN.
func (co CensusOrigin) CurveID() twistededwards.ID {
	switch co {
	case CensusOriginCSPEdDSABN254V1:
		return twistededwards.BN254
	default:
		return twistededwards.UNKNOWN
	}
}

// Valid checks if the CensusOrigin is a valid value.
func (co CensusOrigin) Valid() bool {
	_, ok := supportedCensusOrigins[co]
	return ok
}

// String returns a string representation of the CensusOrigin.
func (co CensusOrigin) String() string {
	if name, ok := supportedCensusOrigins[co]; ok {
		return name
	}
	return CensusOriginNameUnknown
}

// BigInt converts the CensusOrigin to a *types.BigInt representation.
func (co CensusOrigin) BigInt() *BigInt {
	if !co.Valid() {
		return nil
	}
	return (*BigInt)(new(big.Int).SetUint64(uint64(co)))
}

// IsMerkleTree checks if the CensusOrigin corresponds to a Merkle Tree census.
func (co CensusOrigin) IsMerkleTree() bool {
	switch co {
	case CensusOriginMerkleTreeOffchainStaticV1,
		CensusOriginMerkleTreeOffchainDynamicV1,
		CensusOriginMerkleTreeOnchainV1:
		return true
	default:
		return false
	}
}

// IsCSP checks if the CensusOrigin corresponds to a CSP-based census.
func (co CensusOrigin) IsCSP() bool {
	switch co {
	case CensusOriginCSPEdDSABN254V1:
		return true
	default:
		return false
	}
}

// CensusOriginFromString converts a string representation of a CensusOrigin
// to its corresponding CensusOrigin value. If the string does not match any
// known CensusOrigin, it returns CensusOriginUnknown.
func CensusOriginFromString(name string) CensusOrigin {
	for co, coName := range supportedCensusOrigins {
		if coName == name {
			return co
		}
	}
	return CensusOriginUnknown
}

// NormalizedCensusRoot function ensures that the census root is always of a
// fixed length. If its length is not CensusRootLength, it truncates or pads
// it accordingly.
func NormalizedCensusRoot(original HexBytes) HexBytes {
	return original.LeftPad(CensusRootLength)
}

// Census represents the census used in a voting process. It includes the
// origin, root, and URI of the census.
type Census struct {
	// Census origin type:
	CensusOrigin CensusOrigin `json:"censusOrigin" cbor:"0,keyasint,omitempty"`
	// Census root based on census origin:
	//  - CensusOriginMerkleTreeOffchainStaticV1: Merkle Root (fixed).
	//  - CensusOriginMerkleTreeOffchainDynamicV1: Merkle Root (could change
	// 	  via tx).
	//  - CensusOriginCSPEdDSABN254V1: MiMC7 of CSP PubKey (fixed).
	// TODO: Extend with other census origins:
	//  - CensusOriginMerkleTreeOnchainV1: Address of census manager
	//    contract (should be queried on each transition, during state
	//    transitions).
	CensusRoot HexBytes `json:"censusRoot" cbor:"2,keyasint,omitempty"`
	// CensusURI contains the following information depending on the CensusOrigin:
	//  - CensusOriginMerkleTreeOffchainStaticV1: URL where the sequencer can
	// 	  download the census snapshot used to compute the Merkle Proofs.
	//  - CensusOriginMerkleTreeOffchainDynamicV: URL where the sequencer can
	// 	  download the census snapshot used to compute the Merkle Proofs.
	//  - CensusOriginCSPEdDSABN254V1: URL where the voters can generate their
	// 	  signatures.
	// TODO: Extend with other census origins:
	// 	- CensusOriginMerkleTreeOnchainV1: URL where the sequencer can
	// 	  download the census snapshot used to compute the Merkle Proofs.
	CensusURI string `json:"censusURI" cbor:"3,keyasint,omitempty"`
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
	Index    uint64   `json:"index,omitempty"`
	// CSP related fields
	ProcessID *ProcessID `json:"processId,omitempty"`
	PublicKey HexBytes   `json:"publicKey,omitempty"`
	Signature HexBytes   `json:"signature,omitempty"`
}

// CensusRoot represents the census root used in a voting process.
type CensusRoot struct {
	Root HexBytes `json:"root"`
}

// Valid checks that the CensusProof is well-formed
func (cp *CensusProof) Valid() bool {
	if cp == nil || !cp.CensusOrigin.Valid() {
		return false
	}
	switch cp.CensusOrigin {
	case CensusOriginMerkleTreeOffchainStaticV1, CensusOriginMerkleTreeOffchainDynamicV1, CensusOriginMerkleTreeOnchainV1:
		// By default the census proof is not required to this census origin.
		return true
	case CensusOriginCSPEdDSABN254V1:
		return cp.Root != nil && cp.Address != nil && cp.ProcessID != nil &&
			cp.PublicKey != nil && cp.Signature != nil
	default:
		return false
	}
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
