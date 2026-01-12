package sequencer

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark/backend/groth16"
	groth16_bls12377 "github.com/consensys/gnark/backend/groth16/bls12-377"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
)

type mockAggregationStore struct {
	failed   [][]byte
	released [][]byte
}

func (m *mockAggregationStore) MarkVerifiedBallotsFailed(keys ...[]byte) error {
	m.failed = append(m.failed, keys...)
	return nil
}

func (m *mockAggregationStore) ReleaseVerifiedBallotReservations(keys [][]byte) error {
	m.released = append(m.released, keys...)
	return nil
}

type mockAggregationState struct {
	voteIDs   map[string]struct{}
	addresses map[string]struct{}
}

func (m mockAggregationState) ContainsVoteID(voteID *big.Int) bool {
	_, exists := m.voteIDs[voteID.String()]
	return exists
}

func (m mockAggregationState) ContainsAddress(address *types.BigInt) bool {
	_, exists := m.addresses[address.MathBigInt().String()]
	return exists
}

func TestCollectAggregationBatchInputs_SkipsDontCreateHoles(t *testing.T) {
	c := qt.New(t)

	stg := &mockAggregationStore{}
	processState := mockAggregationState{
		voteIDs:   make(map[string]struct{}),
		addresses: make(map[string]struct{}),
	}

	processID := testutil.FixedProcessID()
	ballots := make([]*storage.VerifiedBallot, 0, params.VotesPerBatch+1)
	keys := make([][]byte, 0, params.VotesPerBatch+1)

	for i := 0; i < params.VotesPerBatch+1; i++ {
		voteID := types.HexBytes{byte(i + 1)}
		ballots = append(ballots, &storage.VerifiedBallot{
			VoteID:     voteID,
			Address:    big.NewInt(int64(i + 1)),
			Proof:      new(groth16_bls12377.Proof),
			InputsHash: big.NewInt(int64(1000 + i)),
		})
		keys = append(keys, []byte{0xAA, byte(i + 1)})
	}

	processState.voteIDs[ballots[0].VoteID.BigInt().MathBigInt().String()] = struct{}{}

	proofToRecursion := func(_ groth16.Proof) (stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine], error) {
		return stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{}, nil
	}

	inputs, err := collectAggregationBatchInputs(stg, processID, ballots, keys, processState, false, proofToRecursion, nil)
	c.Assert(err, qt.IsNil)

	c.Assert(len(inputs.AggBallots), qt.Equals, params.VotesPerBatch)
	c.Assert(len(inputs.ProcessedKeys), qt.Equals, params.VotesPerBatch)
	c.Assert(len(inputs.ProofsInputsHashInputs), qt.Equals, params.VotesPerBatch)

	c.Assert(stg.failed, qt.HasLen, 1)
	c.Assert(stg.failed[0], qt.DeepEquals, keys[0])

	c.Assert(stg.released, qt.HasLen, 0)

	c.Assert(inputs.ProcessedKeys[0], qt.DeepEquals, keys[1])
	c.Assert(inputs.AggBallots[0].VoteID, qt.DeepEquals, ballots[1].VoteID)
	c.Assert(inputs.ProofsInputsHashInputs[0].Cmp(ballots[1].InputsHash), qt.Equals, 0)
	c.Assert(inputs.ProcessedKeys[len(inputs.ProcessedKeys)-1], qt.DeepEquals, keys[len(keys)-1])
}
