package circuits_test

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	cryptoelgamal "github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
	"github.com/vocdoni/gnark-crypto-primitives/elgamal"
	"github.com/vocdoni/gnark-crypto-primitives/hash/bn254/mimc7"
)

type voteVerifierInputsHashCircuit struct {
	Process      circuits.Process[frontend.Variable]
	Vote         circuits.Vote[frontend.Variable]
	ExpectedHash frontend.Variable `gnark:",public"`
}

func (c *voteVerifierInputsHashCircuit) Define(api frontend.API) error {
	hFn, err := mimc7.NewMiMC(api)
	if err != nil {
		return err
	}
	if err := hFn.Write(circuits.VoteVerifierInputs(api, c.Process, c.Vote)...); err != nil {
		return err
	}
	api.AssertIsEqual(hFn.Sum(), c.ExpectedHash)
	return nil
}

func TestVoteVerifierInputsHashMatchesOffCircuit(t *testing.T) {
	c := qt.New(t)

	processID := big.NewInt(5)
	censusOrigin := big.NewInt(int64(types.CensusOriginMerkleTreeOffchainStaticV1))
	address := big.NewInt(11)
	weight := big.NewInt(7)
	voteIDBytes := types.HexBytes{0x01, 0x02, 0x03}
	voteID := new(big.Int).SetBytes(voteIDBytes)

	ballotValues := make([]*big.Int, params.FieldsPerBallot*4)
	for i := range ballotValues {
		ballotValues[i] = big.NewInt(int64(100 + i))
	}

	cryptoBallot := cryptoelgamal.NewBallot(curves.New(bjj.CurveType))
	_, err := cryptoBallot.SetBigInts(ballotValues)
	c.Assert(err, qt.IsNil, qt.Commentf("set ballot values"))

	circuitBallot := circuits.Ballot{}
	for i := range params.FieldsPerBallot {
		offset := i * 4
		circuitBallot[i] = elgamal.Ciphertext{
			C1: twistededwards.Point{
				X: ballotValues[offset],
				Y: ballotValues[offset+1],
			},
			C2: twistededwards.Point{
				X: ballotValues[offset+2],
				Y: ballotValues[offset+3],
			},
		}
	}

	ballotModeBig := circuits.BallotMode[*big.Int]{
		NumFields:      big.NewInt(8),
		UniqueValues:   big.NewInt(1),
		MaxValue:       big.NewInt(5),
		MinValue:       big.NewInt(0),
		MaxValueSum:    big.NewInt(20),
		MinValueSum:    big.NewInt(0),
		CostExponent:   big.NewInt(2),
		CostFromWeight: big.NewInt(0),
	}
	encryptionKeyBig := circuits.EncryptionKey[*big.Int]{
		PubKey: [2]*big.Int{big.NewInt(9), big.NewInt(10)},
	}

	inputs := voteverifier.VoteVerifierInputs{
		ProcessID:       processID,
		CensusOrigin:    types.CensusOriginMerkleTreeOffchainStaticV1,
		BallotMode:      ballotModeBig,
		EncryptionKey:   encryptionKeyBig,
		Address:         address,
		VoteID:          voteIDBytes,
		UserWeight:      weight,
		EncryptedBallot: cryptoBallot,
	}
	expectedHash, err := inputs.InputsHash()
	c.Assert(err, qt.IsNil, qt.Commentf("compute off-circuit inputs hash"))

	circuit := &voteVerifierInputsHashCircuit{}
	assignment := &voteVerifierInputsHashCircuit{
		Process: circuits.Process[frontend.Variable]{
			ID:           processID,
			CensusOrigin: censusOrigin,
			BallotMode: circuits.BallotMode[frontend.Variable]{
				NumFields:      ballotModeBig.NumFields,
				UniqueValues:   ballotModeBig.UniqueValues,
				MaxValue:       ballotModeBig.MaxValue,
				MinValue:       ballotModeBig.MinValue,
				MaxValueSum:    ballotModeBig.MaxValueSum,
				MinValueSum:    ballotModeBig.MinValueSum,
				CostExponent:   ballotModeBig.CostExponent,
				CostFromWeight: ballotModeBig.CostFromWeight,
			},
			EncryptionKey: circuits.EncryptionKey[frontend.Variable]{
				PubKey: [2]frontend.Variable{encryptionKeyBig.PubKey[0], encryptionKeyBig.PubKey[1]},
			},
		},
		Vote: circuits.Vote[frontend.Variable]{
			Ballot:     circuitBallot,
			VoteID:     voteID,
			Address:    address,
			VoteWeight: weight,
		},
		ExpectedHash: expectedHash,
	}

	assert := test.NewAssert(t)
	assert.SolvingSucceeded(circuit, assignment,
		test.WithCurves(ecc.BN254),
		test.WithBackends(backend.GROTH16))
}
