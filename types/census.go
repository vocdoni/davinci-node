package types

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/twistededwards"
	"github.com/vocdoni/arbo"
)

// CensusOrigin represents the origin of the census used in a voting process.
type CensusOrigin uint8

const (
	CensusOriginUnknown CensusOrigin = iota
	CensusOriginMerkleTreeOffchainStaticV1
	CensusOriginCSPEdDSABN254V1
	// Unused origins
	CensusOriginCSPEdDSABLS12377V1

	CensusOriginNameUnknown                    = "unknown"
	CensusOriginNameMerkleTreeOffchainStaticV1 = "merkle_tree_offchain_static_v1"
	CensusOriginNameCSPEdDSABN254V1            = "csp_eddsa_bn254_v1"
	// Unused names
	CensusOriginNameCSPEdDSABLS12377V1 = "csp_eddsa_bls12377_v1"

	// CensusRootLength defines the length in bytes of the census root.
	CensusRootLength = 32
)

var supportedCensusOrigins = map[CensusOrigin]string{
	CensusOriginMerkleTreeOffchainStaticV1: CensusOriginNameMerkleTreeOffchainStaticV1,
	CensusOriginCSPEdDSABN254V1:            CensusOriginNameCSPEdDSABN254V1,
}

// CurveID returns the twistededwards.ID associated with the CensusOrigin. Only
// CSP origins have an associated curve, the rest return UNKNOWN.
func (co CensusOrigin) CurveID() twistededwards.ID {
	switch co {
	case CensusOriginCSPEdDSABLS12377V1:
		return twistededwards.BLS12_377
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
	//  - CensusOriginCSPEdDSABN254V1: MiMC7 of CSP PubKey (fixed).
	// TODO: Extend with other census origins:
	//  - CensusOriginMerkleTreeOffchainDynamicV1: Merkle Root (could change
	// 	  via tx).
	//  - CensusOriginMerkleTreeOnchainDynamicV1: Address of census manager
	//    contract (should be queried on each transition, during state
	//    transitions).
	CensusRoot HexBytes `json:"censusRoot" cbor:"2,keyasint,omitempty"`
	// CensusURI contains the following information depending on the CensusOrigin:
	//  - CensusOriginMerkleTreeOffchainStaticV1: URL where the sequencer can
	// 	  download the census snapshot used to compute the Merkle Proofs.
	//  - CensusOriginCSPEdDSABN254V1: URL where the voters can generate their
	// 	  signatures.
	// TODO: Extend with other census origins:
	//  - CensusOriginMerkleTreeOffchainDynamicV: URL where the sequencer can
	// 	  download the census snapshot used to compute the Merkle Proofs.
	// 	- CensusOriginMerkleTreeOnchainDynamicV1: URL where the sequencer can
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
	if cp == nil || !cp.CensusOrigin.Valid() {
		return false
	}
	switch cp.CensusOrigin {
	case CensusOriginMerkleTreeOffchainStaticV1:
		// By default the census proof is not required to this census origin.
		return true
	case CensusOriginCSPEdDSABLS12377V1, CensusOriginCSPEdDSABN254V1:
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

// processCensusRootToBigInt helper converts the census root from its original
// format to a BigInt according to the census origin.
func processCensusRootToBigInt(origin CensusOrigin, root HexBytes) (*BigInt, error) {
	if _, ok := supportedCensusOrigins[origin]; !ok {
		return nil, fmt.Errorf("unsupported census origin: %s", origin)
	}
	if origin == CensusOriginMerkleTreeOffchainStaticV1 {
		return new(BigInt).SetBigInt(arbo.BytesToBigInt(root)), nil
	}
	return root.BigInt(), nil
}
