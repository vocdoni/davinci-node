package sequencer

import (
	"fmt"
	"math/big"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	groth16_bw6761 "github.com/consensys/gnark/backend/groth16/bw6-761"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	"github.com/consensys/gnark/std/math/emulated"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
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
	// Process each registered process ID
	s.pids.ForEach(func(pid []byte, lastUpdate time.Time) bool {
		// Check if this batch is ready for processing
		ballotCount := s.stg.CountVerifiedBallots(pid)

		// If there are no ballots, skip this process ID
		if ballotCount == 0 {
			return true // Continue to next process ID
		}

		// If we have enough ballots for a full batch, process it regardless of time
		if ballotCount >= types.VotesPerBatch {
			return s.processAndUpdateBatch(pid)
		}

		// Otherwise, check if we have a first ballot timestamp and if enough time has passed
		firstBallotTime, hasFirstBallot := s.pids.GetFirstBallotTime(pid)

		// If we don't have a first ballot timestamp yet, set it now
		if !hasFirstBallot {
			s.pids.SetFirstBallotTime(pid)
			return true // Continue to next process ID
		}

		// Check if enough time has passed since the first ballot
		timeSinceFirstBallot := time.Since(firstBallotTime)
		if timeSinceFirstBallot <= s.batchTimeWindow {
			return true // Continue to next process ID
		}

		// If we're here, we have some ballots and the time window has elapsed
		return s.processAndUpdateBatch(pid)
	})
}

// processAndUpdateBatch handles the processing of a batch of ballots and updates
// the necessary timestamps. It returns true to continue processing other process IDs.
func (s *Sequencer) processAndUpdateBatch(pid []byte) bool {
	if err := s.aggregateBatch(pid); err != nil {
		log.Warnw("failed to aggregate batch",
			"error", err.Error(),
			"processID", fmt.Sprintf("%x", pid))
		return true // Continue to next process ID
	}

	// Clear the first ballot timestamp since we've processed the batch
	s.pids.ClearFirstBallotTime(pid)

	return true // Continue to next process ID
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
	s.workInProgressLock.Lock()
	defer s.workInProgressLock.Unlock()

	if len(pid) == 0 {
		return fmt.Errorf("process ID cannot be empty")
	}

	// Pull verified ballots from storage
	ballots, keys, err := s.stg.PullVerifiedBallots(pid, types.VotesPerBatch)
	if err != nil {
		return fmt.Errorf("failed to pull verified ballots: %w", err)
	}

	if len(ballots) == 0 {
		return nil
	}

	log.Debugw("aggregating ballots",
		"processID", fmt.Sprintf("%x", pid),
		"ballotCount", len(ballots),
	)
	startTime := time.Now()

	// Prepare data structures for the aggregator circuit
	proofs := [types.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{}
	proofsInputHash := [types.VotesPerBatch]emulated.Element[sw_bn254.ScalarField]{}
	aggBallots := make([]*storage.AggregatorBallot, 0, len(ballots))
	proofsInputsHashInputs := []*big.Int{}

	// Get the current process state to check if the vote ID already exists
	processState, err := s.latestProcessState(new(types.ProcessID).SetBytes(pid))
	if err != nil {
		return fmt.Errorf("failed to get latest process state: %w", err)
	}
	// Transform each ballot's proof for the aggregator circuit
	var transformErr error
	for i, b := range ballots {
		// if the vote ID already exists in the state, skip it
		if processState.ContainsVoteID(b.VoteID) {
			log.Debugw("skipping ballot with existing vote ID",
				"voteID", b.VoteID.String(),
				"processID", fmt.Sprintf("%x", pid),
			)
			continue
		}

		proofs[i], transformErr = stdgroth16.ValueOfProof[sw_bls12377.G1Affine, sw_bls12377.G2Affine](groth16.Proof(b.Proof))
		if transformErr != nil {
			return fmt.Errorf("failed to transform proof for recursion (ballot %s): %w", b.VoteID.String(), transformErr)
		}
		proofsInputHash[i] = emulated.ValueOf[sw_bn254.ScalarField](b.InputsHash)
		proofsInputsHashInputs = append(proofsInputsHashInputs, b.InputsHash)
		aggBallots = append(aggBallots, &storage.AggregatorBallot{
			VoteID:          b.VoteID,
			Address:         b.Address,
			EncryptedBallot: b.EncryptedBallot,
		})
	}

	// Check if we have some ballots to process
	if len(aggBallots) == 0 {
		log.Debugw("no ballots to process", "processID", fmt.Sprintf("%x", pid))
		return nil
	}

	// Padding the proofsInputsHashInputs with 1s to fill the array
	for i := len(ballots); i < types.VotesPerBatch; i++ {
		proofsInputsHashInputs = append(proofsInputsHashInputs, new(big.Int).SetInt64(1))
	}

	// Compute the hash of the ballot input hashes using MiMC hash function
	inputsHash, err := mimc7.Hash(proofsInputsHashInputs, nil)
	if err != nil {
		return fmt.Errorf("failed to calculate inputs hash: %w", err)
	}

	// Create the aggregator circuit assignment
	assignment := &aggregator.AggregatorCircuit{
		ValidProofs:        len(ballots),
		InputsHash:         emulated.ValueOf[sw_bn254.ScalarField](inputsHash),
		Proofs:             proofs,
		ProofsInputsHashes: proofsInputHash,
	}

	// Fill any remaining slots with dummy proofs if needed
	if len(ballots) < types.VotesPerBatch {
		log.Debugw("filling with dummy proofs", "count", types.VotesPerBatch-len(ballots))
		if err := assignment.FillWithDummy(s.vvCcs, s.vvPk, s.bVkCircom, len(ballots)); err != nil {
			if err := s.stg.MarkVerifiedBallotsFailed(keys...); err != nil {
				log.Warnw("failed to mark ballot batch as failed",
					"error", err.Error(),
					"processID", fmt.Sprintf("%x", pid))
			}
			return fmt.Errorf("failed to fill with dummy proofs: %w", err)
		}
	}

	// Prepare the circuit assignment
	log.Debugw("inputs ready for aggregation", "took", time.Since(startTime).String())
	startTime = time.Now()

	// Prepare the options for the prover
	opts := stdgroth16.GetNativeProverOptions(
		circuits.StateTransitionCurve.ScalarField(),
		circuits.AggregatorCurve.ScalarField(),
	)

	// Generate the proof for the aggregator circuit
	proof, err := s.prover(
		circuits.AggregatorCurve,
		s.aggCcs,
		s.aggPk,
		assignment,
		opts,
	)
	if err != nil {
		if err := s.stg.MarkVerifiedBallotsFailed(keys...); err != nil {
			log.Warnw("failed to mark ballot batch as failed",
				"error", err.Error(),
				"processID", fmt.Sprintf("%x", pid))
		}
		return fmt.Errorf("failed to generate aggregate proof: %w", err)
	}

	log.Infow("aggregate proof generated",
		"took", time.Since(startTime).String(),
		"processID", fmt.Sprintf("%x", pid),
		"ballots", len(ballots),
	)

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
	if err := s.stg.MarkVerifiedBallotsDone(keys...); err != nil {
		if err := s.stg.MarkVerifiedBallotsFailed(keys...); err != nil {
			log.Warnw("failed to mark ballot batch as failed",
				"error", err.Error(),
				"processID", fmt.Sprintf("%x", pid))
		}
		return fmt.Errorf("failed to mark verified ballots as done: %w", err)
	}
	return nil
}
