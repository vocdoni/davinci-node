package statetransition

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/test"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/types/params"
	"github.com/vocdoni/gnark-crypto-primitives/hash/bn254/poseidon"
)

type proofInputsHashCircuit struct {
	Process circuits.Process[frontend.Variable]
	Vote    circuits.Vote[frontend.Variable]
}

func (c *proofInputsHashCircuit) Define(api frontend.API) error {
	st := StateTransitionCircuit{
		Process: c.Process,
	}
	st.Votes[0] = Vote{Vote: c.Vote}
	expected, err := poseidon.MultiHash(api, circuits.BallotHash(api, st.Process, st.Votes[0].Vote)...)
	if err != nil {
		return err
	}
	actual := st.proofInputsHash(api, 0)
	api.AssertIsEqual(actual, expected)
	return nil
}

func TestProofInputsHashUsesPoseidon(t *testing.T) {
	ballot := circuits.NewBallot()
	if ballot == nil {
		t.Fatal("expected ballot to be initialized")
	}

	circuit := &proofInputsHashCircuit{}
	assignment := &proofInputsHashCircuit{
		Process: circuits.Process[frontend.Variable]{
			ID:           big.NewInt(1),
			CensusOrigin: big.NewInt(0),
			BallotMode: circuits.BallotMode[frontend.Variable]{
				NumFields:      big.NewInt(1),
				UniqueValues:   big.NewInt(0),
				MaxValue:       big.NewInt(10),
				MinValue:       big.NewInt(0),
				MaxValueSum:    big.NewInt(10),
				MinValueSum:    big.NewInt(0),
				CostExponent:   big.NewInt(1),
				CostFromWeight: big.NewInt(0),
			},
			EncryptionKey: circuits.EncryptionKey[frontend.Variable]{
				PubKey: [2]frontend.Variable{big.NewInt(2), big.NewInt(3)},
			},
		},
		Vote: circuits.Vote[frontend.Variable]{
			Ballot:     *ballot,
			VoteID:     big.NewInt(4),
			Address:    big.NewInt(5),
			VoteWeight: big.NewInt(6),
		},
	}

	assert := test.NewAssert(t)
	assert.SolvingSucceeded(circuit, assignment,
		test.WithCurves(params.StateTransitionCurve),
		test.WithBackends(backend.GROTH16))
}
