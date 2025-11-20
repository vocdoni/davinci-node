package storage

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/types"
)

func TestCleanAllPending(t *testing.T) {
	c := qt.New(t)

	// Create storage instance
	db := metadb.NewTest(t)
	s := New(db)
	defer s.Close()

	// Create test processes
	pid1 := &types.ProcessID{
		Address: [20]byte{1},
		Nonce:   1,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}
	pid2 := &types.ProcessID{
		Address: [20]byte{2},
		Nonce:   2,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	processID1 := pid1.Marshal()
	processID2 := pid2.Marshal()

	// Initialize processes
	for _, pid := range []*types.ProcessID{pid1, pid2} {
		process := &types.Process{
			ID:             pid.Marshal(),
			Status:         0,
			StartTime:      time.Now(),
			Duration:       time.Hour,
			MetadataURI:    "http://example.com/metadata",
			StateRoot:      new(types.BigInt).SetUint64(100),
			SequencerStats: types.SequencerProcessStats{},
			BallotMode: &types.BallotMode{
				NumFields:   8,
				MaxValue:    new(types.BigInt).SetUint64(100),
				MinValue:    new(types.BigInt).SetUint64(0),
				MaxValueSum: new(types.BigInt).SetUint64(0),
				MinValueSum: new(types.BigInt).SetUint64(0),
			},
			Census: &types.Census{
				CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
				CensusRoot:   make([]byte, 32),
				CensusURI:    "http://example.com/census",
			},
		}
		err := s.NewProcess(process)
		c.Assert(err, qt.IsNil)
	}

	// Helper function to create a verified ballot
	createVerifiedBallot := func(processID []byte, voteID int) *VerifiedBallot {
		voteIDBytes := types.HexBytes(fmt.Sprintf("vote%d", voteID))
		return &VerifiedBallot{
			VoteID:          voteIDBytes,
			ProcessID:       processID,
			VoterWeight:     big.NewInt(1),
			EncryptedBallot: &elgamal.Ballot{},
			Address:         big.NewInt(int64(voteID)),
			InputsHash:      big.NewInt(100),
		}
	}

	// Helper function to create an aggregator ballot
	createAggregatorBallot := func(voteID int) *AggregatorBallot {
		voteIDBytes := types.HexBytes(fmt.Sprintf("vote%d", voteID))
		return &AggregatorBallot{
			VoteID:          voteIDBytes,
			Address:         big.NewInt(int64(voteID)),
			EncryptedBallot: &elgamal.Ballot{},
		}
	}

	// Test 1: Clean verified ballots
	t.Run("CleanVerifiedBallots", func(t *testing.T) {
		c := qt.New(t)

		// Add verified ballots for both processes
		vb1 := createVerifiedBallot(processID1, 1)
		vb2 := createVerifiedBallot(processID1, 2)
		vb3 := createVerifiedBallot(processID2, 3)

		// Store verified ballots directly
		for _, vb := range []*VerifiedBallot{vb1, vb2, vb3} {
			key := append(vb.ProcessID, vb.VoteID...)
			err := s.setArtifact(verifiedBallotPrefix, key, vb)
			c.Assert(err, qt.IsNil)

			// Set vote ID status to verified
			err = s.setVoteIDStatus(vb.ProcessID, vb.VoteID, VoteIDStatusVerified)
			c.Assert(err, qt.IsNil)

			// Lock the vote ID
			s.lockVoteID(vb.VoteID.BigInt().MathBigInt())
		}

		// Update process stats to reflect verified ballots
		err := s.updateProcessStats(processID1, []ProcessStatsUpdate{
			{TypeStats: types.TypeStatsVerifiedVotes, Delta: 2},
			{TypeStats: types.TypeStatsCurrentBatchSize, Delta: 2},
		})
		c.Assert(err, qt.IsNil)

		err = s.updateProcessStats(processID2, []ProcessStatsUpdate{
			{TypeStats: types.TypeStatsVerifiedVotes, Delta: 1},
			{TypeStats: types.TypeStatsCurrentBatchSize, Delta: 1},
		})
		c.Assert(err, qt.IsNil)

		// Verify ballots exist
		c.Assert(s.CountVerifiedBallots(processID1), qt.Equals, 2)
		c.Assert(s.CountVerifiedBallots(processID2), qt.Equals, 1)

		// Clean all pending
		err = s.CleanAllPending()
		c.Assert(err, qt.IsNil)

		// Verify all ballots are cleaned
		c.Assert(s.CountVerifiedBallots(processID1), qt.Equals, 0)
		c.Assert(s.CountVerifiedBallots(processID2), qt.Equals, 0)

		// Verify vote IDs are marked as error
		for _, vb := range []*VerifiedBallot{vb1, vb2, vb3} {
			status, err := s.VoteIDStatus(vb.ProcessID, vb.VoteID)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, VoteIDStatusError)
		}

		// Verify nullifier locks are released
		for _, vb := range []*VerifiedBallot{vb1, vb2, vb3} {
			c.Assert(s.IsVoteIDProcessing(vb.VoteID.BigInt().MathBigInt()), qt.IsFalse)
		}

		// Verify stats are updated
		p1, err := s.Process(pid1)
		c.Assert(err, qt.IsNil)
		c.Assert(p1.SequencerStats.VerifiedVotesCount, qt.Equals, 0)
		c.Assert(p1.SequencerStats.CurrentBatchSize, qt.Equals, 0)

		p2, err := s.Process(pid2)
		c.Assert(err, qt.IsNil)
		c.Assert(p2.SequencerStats.VerifiedVotesCount, qt.Equals, 0)
		c.Assert(p2.SequencerStats.CurrentBatchSize, qt.Equals, 0)
	})

	// Test 2: Clean aggregated batches
	t.Run("CleanAggregatedBatches", func(t *testing.T) {
		c := qt.New(t)

		// Create aggregated batches
		batch1 := &AggregatorBallotBatch{
			ProcessID: processID1,
			Ballots: []*AggregatorBallot{
				createAggregatorBallot(10),
				createAggregatorBallot(11),
			},
		}

		batch2 := &AggregatorBallotBatch{
			ProcessID: processID2,
			Ballots: []*AggregatorBallot{
				createAggregatorBallot(20),
			},
		}

		// Store batches and set vote statuses
		for _, batch := range []*AggregatorBallotBatch{batch1, batch2} {
			err := s.PushAggregatorBatch(batch)
			c.Assert(err, qt.IsNil)

			// Lock vote IDs
			for _, ballot := range batch.Ballots {
				s.lockVoteID(ballot.VoteID.BigInt().MathBigInt())
			}
		}

		// Clean all pending
		err := s.CleanAllPending()
		c.Assert(err, qt.IsNil)

		// Verify batches are cleaned
		_, _, err = s.NextAggregatorBatch(processID1)
		c.Assert(err, qt.Equals, ErrNoMoreElements)

		_, _, err = s.NextAggregatorBatch(processID2)
		c.Assert(err, qt.Equals, ErrNoMoreElements)

		// Verify vote IDs are marked as error
		for _, batch := range []*AggregatorBallotBatch{batch1, batch2} {
			for _, ballot := range batch.Ballots {
				status, err := s.VoteIDStatus(batch.ProcessID, ballot.VoteID)
				c.Assert(err, qt.IsNil)
				c.Assert(status, qt.Equals, VoteIDStatusError)

				// Verify nullifier locks are released
				c.Assert(s.IsVoteIDProcessing(ballot.VoteID.BigInt().MathBigInt()), qt.IsFalse)
			}
		}

		// Verify stats are updated
		p1, err := s.Process(pid1)
		c.Assert(err, qt.IsNil)
		c.Assert(p1.SequencerStats.AggregatedVotesCount, qt.Equals, 0)

		p2, err := s.Process(pid2)
		c.Assert(err, qt.IsNil)
		c.Assert(p2.SequencerStats.AggregatedVotesCount, qt.Equals, 0)
	})

	// Test 3: Clean state transitions
	t.Run("CleanStateTransitions", func(t *testing.T) {
		c := qt.New(t)

		// Create state transition batches
		stb1 := &StateTransitionBatch{
			ProcessID: processID1,
			Ballots: []*AggregatorBallot{
				createAggregatorBallot(30),
				createAggregatorBallot(31),
			},
			Inputs: StateTransitionBatchProofInputs{
				RootHashBefore: big.NewInt(1),
				RootHashAfter:  big.NewInt(2),
				NumNewVotes:    2,
			},
		}

		stb2 := &StateTransitionBatch{
			ProcessID: processID2,
			Ballots: []*AggregatorBallot{
				createAggregatorBallot(40),
			},
			Inputs: StateTransitionBatchProofInputs{
				RootHashBefore: big.NewInt(1),
				RootHashAfter:  big.NewInt(2),
				NumNewVotes:    1,
			},
		}

		// Store state transitions and set vote statuses
		for _, stb := range []*StateTransitionBatch{stb1, stb2} {
			err := s.PushStateTransitionBatch(stb)
			c.Assert(err, qt.IsNil)

			// Lock vote IDs
			for _, ballot := range stb.Ballots {
				s.lockVoteID(ballot.VoteID.BigInt().MathBigInt())
			}
		}

		// Clean all pending
		err := s.CleanAllPending()
		c.Assert(err, qt.IsNil)

		// Verify state transitions are cleaned
		_, _, err = s.NextStateTransitionBatch(processID1)
		c.Assert(err, qt.Equals, ErrNoMoreElements)

		_, _, err = s.NextStateTransitionBatch(processID2)
		c.Assert(err, qt.Equals, ErrNoMoreElements)

		// Verify vote IDs remain in PROCESSED status (not marked as error)
		// PROCESSED votes are valid and just waiting for settlement
		for _, stb := range []*StateTransitionBatch{stb1, stb2} {
			for _, ballot := range stb.Ballots {
				status, err := s.VoteIDStatus(stb.ProcessID, ballot.VoteID)
				c.Assert(err, qt.IsNil)
				c.Assert(status, qt.Equals, VoteIDStatusProcessed, qt.Commentf("state transition votes should remain PROCESSED, not ERROR"))

				// Verify nullifier locks are released
				c.Assert(s.IsVoteIDProcessing(ballot.VoteID.BigInt().MathBigInt()), qt.IsFalse)
			}
		}

		// Verify stats are updated
		p1, err := s.Process(pid1)
		c.Assert(err, qt.IsNil)
		c.Assert(p1.SequencerStats.StateTransitionCount, qt.Equals, 0)

		p2, err := s.Process(pid2)
		c.Assert(err, qt.IsNil)
		c.Assert(p2.SequencerStats.StateTransitionCount, qt.Equals, 0)
	})

	// Test 4: Clean all types together
	t.Run("CleanAllTypesTogether", func(t *testing.T) {
		c := qt.New(t)

		// Add verified ballots
		vb := createVerifiedBallot(processID1, 50)
		err := s.setArtifact(verifiedBallotPrefix, append(vb.ProcessID, vb.VoteID...), vb)
		c.Assert(err, qt.IsNil)
		err = s.setVoteIDStatus(vb.ProcessID, vb.VoteID, VoteIDStatusVerified)
		c.Assert(err, qt.IsNil)
		s.lockVoteID(vb.VoteID.BigInt().MathBigInt())

		// Add aggregated batch
		batch := &AggregatorBallotBatch{
			ProcessID: processID1,
			Ballots:   []*AggregatorBallot{createAggregatorBallot(51)},
		}
		err = s.PushAggregatorBatch(batch)
		c.Assert(err, qt.IsNil)
		s.lockVoteID(batch.Ballots[0].VoteID.BigInt().MathBigInt())

		// Add state transition
		stb := &StateTransitionBatch{
			ProcessID: processID1,
			Ballots:   []*AggregatorBallot{createAggregatorBallot(52)},
			Inputs: StateTransitionBatchProofInputs{
				RootHashBefore: big.NewInt(1),
				RootHashAfter:  big.NewInt(2),
				NumNewVotes:    1,
			},
		}
		err = s.PushStateTransitionBatch(stb)
		c.Assert(err, qt.IsNil)
		s.lockVoteID(stb.Ballots[0].VoteID.BigInt().MathBigInt())

		// Clean all pending
		err = s.CleanAllPending()
		c.Assert(err, qt.IsNil)

		// Verify all are cleaned
		c.Assert(s.CountVerifiedBallots(processID1), qt.Equals, 0)

		_, _, err = s.NextAggregatorBatch(processID1)
		c.Assert(err, qt.Equals, ErrNoMoreElements)

		_, _, err = s.NextStateTransitionBatch(processID1)
		c.Assert(err, qt.Equals, ErrNoMoreElements)

		// Verify vote IDs status and locks released
		// Verified and aggregated ballots should be ERROR
		status, err := s.VoteIDStatus(processID1, vb.VoteID)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, VoteIDStatusError)
		c.Assert(s.IsVoteIDProcessing(vb.VoteID.BigInt().MathBigInt()), qt.IsFalse)

		status, err = s.VoteIDStatus(processID1, batch.Ballots[0].VoteID)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, VoteIDStatusError)
		c.Assert(s.IsVoteIDProcessing(batch.Ballots[0].VoteID.BigInt().MathBigInt()), qt.IsFalse)

		// State transition ballot should remain PROCESSED (not ERROR)
		status, err = s.VoteIDStatus(processID1, stb.Ballots[0].VoteID)
		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.Equals, VoteIDStatusProcessed, qt.Commentf("state transition votes should remain PROCESSED"))
		c.Assert(s.IsVoteIDProcessing(stb.Ballots[0].VoteID.BigInt().MathBigInt()), qt.IsFalse)
	})

	// Test 5: Clean with mixed vote statuses
	t.Run("CleanWithMixedStatuses", func(t *testing.T) {
		c := qt.New(t)

		// Get current stats to know the baseline
		pBefore, err := s.Process(pid1)
		c.Assert(err, qt.IsNil)
		initialVerifiedCount := pBefore.SequencerStats.VerifiedVotesCount

		// Add verified ballot with correct status
		vb1 := createVerifiedBallot(processID1, 60)
		err = s.setArtifact(verifiedBallotPrefix, append(vb1.ProcessID, vb1.VoteID...), vb1)
		c.Assert(err, qt.IsNil)
		err = s.setVoteIDStatus(vb1.ProcessID, vb1.VoteID, VoteIDStatusVerified)
		c.Assert(err, qt.IsNil)

		// Add verified ballot with wrong status (already aggregated)
		vb2 := createVerifiedBallot(processID1, 61)
		err = s.setArtifact(verifiedBallotPrefix, append(vb2.ProcessID, vb2.VoteID...), vb2)
		c.Assert(err, qt.IsNil)
		err = s.setVoteIDStatus(vb2.ProcessID, vb2.VoteID, VoteIDStatusAggregated)
		c.Assert(err, qt.IsNil)

		// Update stats for only the correctly-statused ballot
		err = s.updateProcessStats(processID1, []ProcessStatsUpdate{
			{TypeStats: types.TypeStatsVerifiedVotes, Delta: 1},
		})
		c.Assert(err, qt.IsNil)

		// Clean all pending
		err = s.CleanAllPending()
		c.Assert(err, qt.IsNil)

		// Both should be cleaned
		c.Assert(s.CountVerifiedBallots(processID1), qt.Equals, 0)

		// Both should be marked as error
		for _, vb := range []*VerifiedBallot{vb1, vb2} {
			status, err := s.VoteIDStatus(vb.ProcessID, vb.VoteID)
			c.Assert(err, qt.IsNil)
			c.Assert(status, qt.Equals, VoteIDStatusError)
		}

		// Stats should only decrease by 1 (the correctly-statused ballot)
		// So it should be back to the initial count
		p, err := s.Process(pid1)
		c.Assert(err, qt.IsNil)
		c.Assert(p.SequencerStats.VerifiedVotesCount, qt.Equals, initialVerifiedCount)
	})
}
