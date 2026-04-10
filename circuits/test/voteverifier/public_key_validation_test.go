package voteverifiertest

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	ballottest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

func TestVerifyVoteCircuitRejectsOffCurvePublicKey(t *testing.T) {
	c := qt.New(t)

	placeholder, err := voteverifier.DummyPlaceholder()
	c.Assert(err, qt.IsNil)
	assignment, err := voteverifier.DummyAssignment()
	c.Assert(err, qt.IsNil)

	assignment.PublicKey.X = emulated.ValueOf[emulated.Secp256k1Fp](1)
	assignment.PublicKey.Y = emulated.ValueOf[emulated.Secp256k1Fp](1)

	assert := test.NewAssert(t)
	assert.SolvingFailed(placeholder, assignment,
		test.WithCurves(ecc.BLS12_377),
		test.WithBackends(backend.GROTH16))
}

func TestVerifyVoteCircuitRejectsNonCanonicalAddressAlias(t *testing.T) {
	c := qt.New(t)

	signer, err := ballottest.GenDeterministicECDSAaccountForTest(0)
	c.Assert(err, qt.IsNil)

	_, placeholder, assignments := VoteVerifierInputsForTest(t, []VoterTestData{
		{
			PrivKey: signer,
			PubKey:  signer.PublicKey,
			Address: signer.Address(),
		},
	}, testutil.FixedProcessID(), types.CensusOriginMerkleTreeOffchainStaticV1)

	assignment := assignments[0]
	overflowAddress := new(big.Int).Add(
		new(big.Int).SetBytes(signer.Address().Bytes()),
		params.VoteVerifierCurve.ScalarField(),
	)
	assignment.Address = emulated.ValueOf[sw_bn254.ScalarField](overflowAddress)

	assert := test.NewAssert(t)
	assert.SolvingFailed(&placeholder, &assignment,
		test.WithCurves(ecc.BLS12_377),
		test.WithBackends(backend.GROTH16))
}
