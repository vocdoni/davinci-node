package sequencer

import (
	"math/big"
	"testing"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	groth16_bls12377 "github.com/consensys/gnark/backend/groth16/bls12-377"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

// TestBatchTimingBehavior tests the core behavior of our timing update:
// - Time window only starts counting when there's at least one ballot
// - Timer is reset after processing a batch
func TestBatchTimingBehavior(t *testing.T) {
	// This test verifies the logic flow, not actual dependencies
	c := qt.New(t)

	// Create a ProcessIDMap for testing
	pmap := NewProcessIDMap()
	processID := testutil.RandomProcessID()

	// 1. Initially, there should be no first ballot timestamp
	_, exists := pmap.GetFirstBallotTime(processID)
	c.Assert(exists, qt.Equals, false, qt.Commentf("Initially, there should be no first ballot timestamp"))

	// Set the first ballot time
	startTime := time.Now()
	pmap.SetFirstBallotTime(processID)
	initialTime, exists := pmap.GetFirstBallotTime(processID)
	c.Assert(exists, qt.Equals, true, qt.Commentf("After setting, first ballot timestamp should exist"))

	// Check time is recent (within 1 second of now)
	c.Assert(initialTime.After(startTime.Add(-time.Second)), qt.IsTrue,
		qt.Commentf("First ballot time should be recent"))

	// Sleep a bit to ensure time difference
	time.Sleep(10 * time.Millisecond)

	// 3. After processing a batch, the timestamp should be cleared
	pmap.ClearFirstBallotTime(processID)
	_, exists = pmap.GetFirstBallotTime(processID)
	c.Assert(exists, qt.Equals, false, qt.Commentf("After clearing, timestamp should not exist"))

	// 4. When next ballot arrives, a new timestamp should be set
	newStartTime := time.Now()
	pmap.SetFirstBallotTime(processID)
	newTime, exists := pmap.GetFirstBallotTime(processID)
	c.Assert(exists, qt.Equals, true, qt.Commentf("After setting again, timestamp should exist"))
	c.Assert(newTime.After(newStartTime.Add(-time.Second)), qt.IsTrue,
		qt.Commentf("New first ballot time should be recent"))
}

// TestAggregatePreflightQuarantine verifies that when a batch contains N valid
// ballots plus one ballot whose address is absent from the census, the
// pre-flight census check in collectAggregationBatchInputs:
//  1. Quarantines the absent-address ballot by marking it failed in storage.
//  2. Passes the N valid ballots through to the aggregation inputs unchanged.
//
// This covers AC7 from the plan.
func TestAggregatePreflightQuarantine(t *testing.T) {
	c := qt.New(t)

	stg := &mockAggregationStore{}
	processState := mockAggregationState{
		voteIDs:   make(map[string]struct{}),
		addresses: make(map[string]struct{}),
	}

	processID := testutil.FixedProcessID()

	// N = 3 valid ballots whose addresses are "in census".
	// 1 additional ballot whose address is deliberately absent.
	const n = 3
	absentAddress := big.NewInt(0xDEAD)

	ballots := make([]*storage.VerifiedBallot, 0, n+1)
	keys := make([][]byte, 0, n+1)

	for i := range n {
		ballots = append(ballots, &storage.VerifiedBallot{
			VoteID:     types.VoteID(i + 1),
			Address:    big.NewInt(int64(i + 1)),
			Proof:      new(groth16_bls12377.Proof),
			InputsHash: big.NewInt(int64(1000 + i)),
		})
		keys = append(keys, []byte{0xCC, byte(i)})
	}
	absentKey := []byte{0xCC, byte(n)}
	ballots = append(ballots, &storage.VerifiedBallot{
		VoteID:     types.VoteID(n + 1),
		Address:    absentAddress,
		Proof:      new(groth16_bls12377.Proof),
		InputsHash: big.NewInt(9999),
	})
	keys = append(keys, absentKey)

	// checkCensusMembership returns false only for absentAddress.
	checkCensus := func(b *storage.VerifiedBallot) bool {
		return b.Address.Cmp(absentAddress) != 0
	}

	proofToRecursion := func(_ groth16.Proof) (stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine], error) {
		return stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{}, nil
	}

	inputs, err := collectAggregationBatchInputs(
		stg,
		processID,
		ballots,
		keys,
		processState,
		false, // maxVotersReached
		proofToRecursion,
		nil, // verifyVoteVerifierProof — skip ZK proof check in this unit test
		checkCensus,
	)
	c.Assert(err, qt.IsNil)

	// The N valid ballots must proceed to aggregation.
	c.Assert(inputs.AggBallots, qt.HasLen, n,
		qt.Commentf("N valid ballots must proceed to aggregation"))
	c.Assert(inputs.ProcessedKeys, qt.HasLen, n)
	c.Assert(inputs.ProofsInputsHashInputs, qt.HasLen, n)

	// Each of the N valid ballots must appear in order.
	for i := range n {
		c.Assert(inputs.AggBallots[i].VoteID, qt.Equals, ballots[i].VoteID,
			qt.Commentf("valid ballot %d must be in aggregation inputs", i))
		c.Assert(inputs.ProcessedKeys[i], qt.DeepEquals, keys[i])
	}

	// The absent-address ballot must be quarantined: exactly one key in failed.
	c.Assert(stg.failed, qt.HasLen, 1,
		qt.Commentf("absent-address ballot must be quarantined (marked failed)"))
	c.Assert(stg.failed[0], qt.DeepEquals, absentKey,
		qt.Commentf("the quarantined key must be the absent-address ballot's storage key"))

	// Nothing should be in released — valid ballots are consumed, not released.
	c.Assert(stg.released, qt.HasLen, 0)
}
