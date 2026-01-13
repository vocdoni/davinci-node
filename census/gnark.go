package census

import (
	"github.com/consensys/gnark-crypto/ecc/twistededwards"
	"github.com/consensys/gnark/frontend"
	"github.com/vocdoni/davinci-node/types"
)

// IsMerkleTreeCensusOrigin returns a frontend.Variable that is 1 if the
// provided origin corresponds to a Merkle Tree census origin, 0 otherwise.
// The supported Merkle Tree census origins are:
//   - CensusOriginMerkleTreeOffchainStaticV1
//   - CensusOriginMerkleTreeOffchainDynamicV1
//   - CensusOriginMerkleTreeOnchainV1
func IsMerkleTreeCensusOrigin(api frontend.API, origin frontend.Variable) frontend.Variable {
	return api.Or(
		api.Or(
			api.IsZero(api.Sub(origin, uint8(types.CensusOriginMerkleTreeOffchainStaticV1))),
			api.IsZero(api.Sub(origin, uint8(types.CensusOriginMerkleTreeOffchainDynamicV1))),
		),
		api.IsZero(api.Sub(origin, uint8(types.CensusOriginMerkleTreeOnchainDynamicV1))),
	)
}

// IsCSPCensusOrigin returns a frontend.Variable that is 1 if the provided
// origin corresponds to a CSP census origin, 0 otherwise.
// The supported CSP census origin is:
//   - CensusOriginCSPEdDSABN254V1
func IsCSPCensusOrigin(api frontend.API, origin frontend.Variable) frontend.Variable {
	return api.IsZero(api.Sub(origin, uint8(types.CensusOriginCSPEdDSABabyJubJubV1)))
}

// CSPCensusOriginCurveID returns the twistededwards.ID corresponding to the
// curve used by the CSP census origin.
func CSPCensusOriginCurveID() twistededwards.ID {
	return types.CensusOriginCSPEdDSABabyJubJubV1.CurveID()
}
