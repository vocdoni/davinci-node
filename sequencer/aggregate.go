package sequencer

import (
	"fmt"
	"maps"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	groth16_bw6761 "github.com/consensys/gnark/backend/groth16/bw6-761"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	"github.com/consensys/gnark/std/math/emulated"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/aggregator"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// startAggregateProcessor starts a background goroutine that periodically checks
// for batches of verified ballots that are ready to be aggregated into a single proof.
// A batch is considered ready when either:
// 1. It contains at least VotesPerBatch ballots, or
// 2. The time since the last update exceeds maxTimeWindow
//
// The processor runs until the sequencer's context is canceled.
func (s *Sequencer) startAggregateProcessor(tickerInterval time.Duration) error {
	ticker := time.NewTicker(tickerInterval)

	go func() {
		defer ticker.Stop()
		log.Infow("aggregate processor started", "tickInterval", tickerInterval)

		for {
			select {
			case <-s.ctx.Done():
				log.Infow("aggregate processor stopped")
				return
			case <-ticker.C:
				s.processPendingBatches()
			}
		}
	}()
	return nil
}

// processPendingBatches checks all registered process IDs and aggregates
// any batches that are ready for processing.
func (s *Sequencer) processPendingBatches() {
	// Copy pids to avoid locking the map for too long
	s.pidsLock.RLock()
	pids := make(map[string]time.Time, len(s.pids))
	maps.Copy(pids, s.pids)
	s.pidsLock.RUnlock()

	// Iterate over the process IDs and process the ones that are ready
	for pid, lastUpdate := range pids {
		// Check if this batch is ready for processing
		ballotCount := s.stg.CountVerifiedBallots([]byte(pid))
		timeSinceUpdate := time.Since(lastUpdate)

		// Skip if the batch is not ready
		if ballotCount == 0 || (ballotCount < types.VotesPerBatch && timeSinceUpdate <= s.maxTimeWindow) {
			continue
		}

		// Process the batch
		log.Debugw("batch ready for aggregation",
			"processID", fmt.Sprintf("%x", pid),
			"ballotCount", ballotCount,
			"timeSinceUpdate", timeSinceUpdate.String(),
			"maxTimeWindow", s.maxTimeWindow,
		)

		startTime := time.Now()
		if err := s.aggregateBatch([]byte(pid)); err != nil {
			log.Warnw("failed to aggregate batch",
				"error", err.Error(),
				"processID", fmt.Sprintf("%x", pid),
			)
			continue
		}
		log.Infow("batch aggregated successfully", "processID", fmt.Sprintf("%x", pid), "took(s)", time.Since(startTime).Seconds())

		// Update the last update time
		s.pidsLock.Lock()
		s.pids[pid] = time.Now()
		s.pidsLock.Unlock()

	}
}

// aggregateBatch creates an aggregated zero-knowledge proof for a batch of verified ballots.
// It pulls verified ballots for the specified process ID, transforms them into a format
// suitable for the aggregator circuit, generates a proof, and stores the result.
//
// Parameters:
//   - pid: The process ID for which to aggregate ballots
//
// Returns an error if the aggregation process fails at any step.
func (s *Sequencer) aggregateBatch(pid types.HexBytes) error {
	if len(pid) == 0 {
		return fmt.Errorf("process ID cannot be empty")
	}

	// Pull verified ballots from storage
	ballots, keys, err := s.stg.PullVerifiedBallots(pid, types.VotesPerBatch)
	if err != nil {
		return fmt.Errorf("failed to pull verified ballots: %w", err)
	}

	if len(ballots) == 0 {
		log.Warnw("no ballots to aggregate", "processID", fmt.Sprintf("%x", pid))
		return nil
	}

	log.Debugw("aggregating ballots",
		"processID", fmt.Sprintf("%x", pid),
		"ballotCount", len(ballots),
	)

	// Prepare data structures for the aggregator circuit
	proofs := [types.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{}
	proofsInputHash := [types.VotesPerBatch]emulated.Element[sw_bn254.ScalarField]{}
	aggBallots := make([]*storage.AggregatorBallot, 0, len(ballots))

	// Transform each ballot's proof for the aggregator circuit
	startTime := time.Now()
	var transformErr error
	for i := range ballots {
		proofs[i], transformErr = stdgroth16.ValueOfProof[sw_bls12377.G1Affine, sw_bls12377.G2Affine](groth16.Proof(ballots[i].Proof))
		if transformErr != nil {
			return fmt.Errorf("failed to transform proof for recursion (ballot %d): %w", i, transformErr)
		}

		proofsInputHash[i] = emulated.ValueOf[sw_bn254.ScalarField](ballots[i].InputsHash)
		log.Debugw("ballot transformed for aggregation", "index", i, "inputsHash", ballots[i].InputsHash.String())
		aggBallots = append(aggBallots, &storage.AggregatorBallot{
			Nullifier:       ballots[i].Nullifier,
			Commitment:      ballots[i].Commitment,
			Address:         ballots[i].Address,
			EncryptedBallot: ballots[i].EncryptedBallot,
		})
	}

	// Create the aggregator circuit assignment
	assignment := aggregator.AggregatorCircuit{
		ValidProofs:        len(ballots),
		Proofs:             proofs,
		ProofsInputsHashes: proofsInputHash,
	}

	// Fill any remaining slots with dummy proofs if needed
	if len(ballots) < types.VotesPerBatch {
		log.Debugw("filling with dummy proofs", "count", types.VotesPerBatch-len(ballots))
		if err := assignment.FillWithDummy(s.voteCcs, s.voteProvingKey, s.ballotVerifyingKeyCircomJSON, len(ballots)); err != nil {
			return fmt.Errorf("failed to fill with dummy proofs: %w", err)
		}
	}

	// Generate the aggregated proof
	witness, err := frontend.NewWitness(assignment, ecc.BW6_761.ScalarField())
	if err != nil {
		return fmt.Errorf("failed to create witness: %w", err)
	}
	log.Debugw("inputs ready for aggregation", "took", time.Since(startTime).String())

	startTime = time.Now()
	proof, err := groth16.Prove(
		s.aggregateCcs,
		s.aggregateProvingKey,
		witness,
		stdgroth16.GetNativeProverOptions(
			circuits.StateTransitionCurve.ScalarField(),
			circuits.AggregatorCurve.ScalarField(),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to generate aggregate proof: %w", err)
	}
	log.Debugw("aggregate proof generated", "took", time.Since(startTime).String())
	// Store the aggregated batch
	abb := storage.AggregatorBallotBatch{
		ProcessID: pid,
		Proof:     proof.(*groth16_bw6761.Proof),
		Ballots:   aggBallots,
	}

	log.Debugw("pushing aggregated batch to storage")
	if err := s.stg.PushBallotBatch(&abb); err != nil {
		return fmt.Errorf("failed to push ballot batch: %w", err)
	}

	// Mark the individual ballots as processed
	failedMarks := 0
	for _, k := range keys {
		if err := s.stg.MarkVerifiedBallotDone(k); err != nil {
			failedMarks++
			log.Warnw("failed to mark verified ballot as done",
				"error", err.Error(),
				"processID", fmt.Sprintf("%x", pid),
			)
		}
	}

	if failedMarks > 0 {
		log.Warnw("some ballots could not be marked as done",
			"failedCount", failedMarks,
			"totalCount", len(keys),
			"processID", fmt.Sprintf("%x", pid),
		)
	}

	log.Infow("batch aggregated successfully",
		"processID", fmt.Sprintf("%x", pid),
		"ballotCount", len(ballots),
	)

	return nil
}

// MockT mimics testing.T behavior
type MockT struct{}

func (t *MockT) Errorf(format string, args ...interface{}) {
	log.Debugf("Assertion Error: "+format, args...)
}
