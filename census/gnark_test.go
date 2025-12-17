package census

import (
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/test"
	"github.com/vocdoni/davinci-node/types"
)

type testCensusOriginCircuit struct {
	OffchainStatic   frontend.Variable
	OffchainnDynamic frontend.Variable
	Onchain          frontend.Variable
	CSP              frontend.Variable
	Unknown          frontend.Variable
}

func (c *testCensusOriginCircuit) Define(api frontend.API) error {
	api.AssertIsEqual(IsMerkleTreeCensusOrigin(api, c.OffchainStatic), 1)
	api.AssertIsEqual(IsMerkleTreeCensusOrigin(api, c.OffchainnDynamic), 1)
	api.AssertIsEqual(IsMerkleTreeCensusOrigin(api, c.Onchain), 1)
	api.AssertIsEqual(IsCSPCensusOrigin(api, c.CSP), 1)
	api.AssertIsEqual(IsMerkleTreeCensusOrigin(api, c.Unknown), 0)
	api.AssertIsEqual(IsCSPCensusOrigin(api, c.Unknown), 0)
	return nil
}

func TestGnarkCensusOrigin(t *testing.T) {
	witness := &testCensusOriginCircuit{
		OffchainStatic:   uint8(types.CensusOriginMerkleTreeOffchainStaticV1),
		OffchainnDynamic: uint8(types.CensusOriginMerkleTreeOffchainDynamicV1),
		Onchain:          uint8(types.CensusOriginMerkleTreeOnchainV1),
		CSP:              uint8(types.CensusOriginCSPEdDSABN254V1),
		Unknown:          uint8(types.CensusOriginUnknown),
	}

	// generate proof
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&testCensusOriginCircuit{}, witness,
		test.WithCurves(ecc.BN254),
		test.WithBackends(backend.GROTH16))
}
