package sequencer

import (
	"fmt"
	"math/big"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	groth16_bw6761 "github.com/consensys/gnark/backend/groth16/bw6-761"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	"github.com/consensys/gnark/std/math/emulated"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
)

type aggregationProcessState interface {
	ContainsVoteID(voteID *big.Int) bool
	ContainsAddress(address *types.BigInt) bool
}

type aggregationStorage interface {
	MarkVerifiedBallotsFailed(keys ...[]byte) error
	ReleaseVerifiedBallotReservations(keys [][]byte) error
}

type (
	proofToRecursionFn           func(groth16.Proof) (stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine], error)
	voteVerifierProofValidatorFn func(ballot *storage.VerifiedBallot) error
)

type aggregationBatchInputs struct {
	proofs                 [params.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]
	proofsInputHash        [params.VotesPerBatch]emulated.Element[sw_bn254.ScalarField]
	aggBallots             []*storage.AggregatorBallot
	verifiedBallots        []*storage.VerifiedBallot
	processedKeys          [][]byte
	proofsInputsHashInputs []*big.Int
}

func collectAggregationBatchInputs(
	stg aggregationStorage,
	pid types.HexBytes,
	ballots []*storage.VerifiedBallot,
	keys [][]byte,
	processState aggregationProcessState,
	maxVotersReached bool,
	proofToRecursion proofToRecursionFn,
	verifyVoteVerifierProof voteVerifierProofValidatorFn,
) (*aggregationBatchInputs, error) {
	// Prepare data structures for the aggregator circuit
	proofs := [params.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{}
	proofsInputHash := [params.VotesPerBatch]emulated.Element[sw_bn254.ScalarField]{}
	aggBallots := make([]*storage.AggregatorBallot, 0, len(ballots))
	verifiedBallots := make([]*storage.VerifiedBallot, 0, len(ballots))
	processedKeys := make([][]byte, 0, params.VotesPerBatch)
	proofsInputsHashInputs := make([]*big.Int, 0, params.VotesPerBatch)

	for i, b := range ballots {
		if b == nil {
			log.Warnw("skipping nil verified ballot",
				"processID", pid.String(),
				"index", i,
			)
			if i < len(keys) {
				if err := stg.MarkVerifiedBallotsFailed(keys[i]); err != nil {
					log.Warnw("failed to mark nil ballot as failed",
						"error", err.Error(),
						"processID", pid.String(),
						"index", i,
					)
					if err := stg.ReleaseVerifiedBallotReservations([][]byte{keys[i]}); err != nil {
						log.Warnw("failed to release ballot reservation after nil ballot failure marking",
							"error", err.Error(),
							"processID", pid.String(),
							"index", i,
						)
					}
				}
			}
			continue
		}

		if b.VoteID == nil {
			addressStr := ""
			if b.Address != nil {
				addressStr = b.Address.String()
			}
			log.Warnw("skipping verified ballot with missing voteID",
				"processID", pid.String(),
				"index", i,
				"address", addressStr,
			)
			if i < len(keys) {
				if err := stg.MarkVerifiedBallotsFailed(keys[i]); err != nil {
					log.Warnw("failed to mark ballot as failed",
						"error", err.Error(),
						"processID", pid.String(),
						"index", i,
					)
					if err := stg.ReleaseVerifiedBallotReservations([][]byte{keys[i]}); err != nil {
						log.Warnw("failed to release ballot reservation after failure marking",
							"error", err.Error(),
							"processID", pid.String(),
							"index", i,
						)
					}
				}
			}
			continue
		}
		if b.Address == nil {
			log.Warnw("skipping verified ballot with missing address",
				"processID", pid.String(),
				"index", i,
				"voteID", b.VoteID.String(),
			)
			if i < len(keys) {
				if err := stg.MarkVerifiedBallotsFailed(keys[i]); err != nil {
					log.Warnw("failed to mark ballot as failed",
						"error", err.Error(),
						"processID", pid.String(),
						"voteID", b.VoteID.String(),
					)
					if err := stg.ReleaseVerifiedBallotReservations([][]byte{keys[i]}); err != nil {
						log.Warnw("failed to release ballot reservation after failure marking",
							"error", err.Error(),
							"processID", pid.String(),
							"voteID", b.VoteID.String(),
						)
					}
				}
			}
			continue
		}

		// if the vote ID already exists in the state, skip it
		if processState.ContainsVoteID(b.VoteID.BigInt().MathBigInt()) {
			log.Debugw("skipping ballot already in state",
				"processID", pid.String(),
				"voteID", b.VoteID.String(),
			)
			if err := stg.MarkVerifiedBallotsFailed(keys[i]); err != nil {
				log.Warnw("failed to mark ballot as failed",
					"error", err.Error(),
					"processID", pid.String(),
					"voteID", b.VoteID.String(),
				)
				if err := stg.ReleaseVerifiedBallotReservations([][]byte{keys[i]}); err != nil {
					log.Warnw("failed to release ballot reservation after failure marking",
						"error", err.Error(),
						"processID", pid.String(),
						"voteID", b.VoteID.String(),
					)
				}
			}
			continue
		}

		// If the maxVoters is reached, check if the ballot is an overwrite
		// and skip if not
		if maxVotersReached && !processState.ContainsAddress(new(types.BigInt).SetBigInt(b.Address)) {
			log.Debugw("skipping ballot due to max voters reached",
				"address", b.Address.String(),
				"processID", pid.String())
			if err := stg.MarkVerifiedBallotsFailed(keys[i]); err != nil {
				log.Warnw("failed to mark ballot as failed",
					"error", err.Error(),
					"processID", pid.String(),
					"voteID", b.VoteID.String(),
					"address", b.Address.String(),
				)
				if err := stg.ReleaseVerifiedBallotReservations([][]byte{keys[i]}); err != nil {
					log.Warnw("failed to release ballot reservation after failure marking",
						"error", err.Error(),
						"processID", pid.String(),
						"voteID", b.VoteID.String(),
						"address", b.Address.String(),
					)
				}
			}
			continue
		}

		if b.Proof == nil {
			log.Warnw("skipping verified ballot with missing vote verifier proof",
				"processID", pid.String(),
				"voteID", b.VoteID.String(),
				"address", b.Address.String(),
			)
			if err := stg.MarkVerifiedBallotsFailed(keys[i]); err != nil {
				log.Warnw("failed to mark ballot as failed",
					"error", err.Error(),
					"processID", pid.String(),
					"voteID", b.VoteID.String(),
					"address", b.Address.String(),
				)
				if err := stg.ReleaseVerifiedBallotReservations([][]byte{keys[i]}); err != nil {
					log.Warnw("failed to release ballot reservation after failure marking",
						"error", err.Error(),
						"processID", pid.String(),
						"voteID", b.VoteID.String(),
						"address", b.Address.String(),
					)
				}
			}
			continue
		}
		if b.InputsHash == nil {
			log.Warnw("skipping verified ballot with missing vote verifier inputs hash",
				"processID", pid.String(),
				"voteID", b.VoteID.String(),
				"address", b.Address.String(),
			)
			if err := stg.MarkVerifiedBallotsFailed(keys[i]); err != nil {
				log.Warnw("failed to mark ballot as failed",
					"error", err.Error(),
					"processID", pid.String(),
					"voteID", b.VoteID.String(),
					"address", b.Address.String(),
				)
				if err := stg.ReleaseVerifiedBallotReservations([][]byte{keys[i]}); err != nil {
					log.Warnw("failed to release ballot reservation after failure marking",
						"error", err.Error(),
						"processID", pid.String(),
						"voteID", b.VoteID.String(),
						"address", b.Address.String(),
					)
				}
			}
			continue
		}

		if !b.Proof.Ar.IsInSubGroup() || !b.Proof.Krs.IsInSubGroup() || !b.Proof.Bs.IsInSubGroup() {
			log.Warnw("skipping verified ballot with malformed vote verifier proof (subgroup check failed)",
				"processID", pid.String(),
				"voteID", b.VoteID.String(),
				"address", b.Address.String(),
			)
			if err := stg.MarkVerifiedBallotsFailed(keys[i]); err != nil {
				log.Warnw("failed to mark ballot as failed",
					"error", err.Error(),
					"processID", pid.String(),
					"voteID", b.VoteID.String(),
					"address", b.Address.String(),
				)
				if err := stg.ReleaseVerifiedBallotReservations([][]byte{keys[i]}); err != nil {
					log.Warnw("failed to release ballot reservation after failure marking",
						"error", err.Error(),
						"processID", pid.String(),
						"voteID", b.VoteID.String(),
						"address", b.Address.String(),
					)
				}
			}
			continue
		}

		if verifyVoteVerifierProof != nil {
			if err := verifyVoteVerifierProof(b); err != nil {
				log.Warnw("skipping verified ballot with invalid vote verifier proof",
					"processID", pid.String(),
					"voteID", b.VoteID.String(),
					"address", b.Address.String(),
					"error", err.Error(),
				)
				if err := stg.MarkVerifiedBallotsFailed(keys[i]); err != nil {
					log.Warnw("failed to mark ballot as failed",
						"error", err.Error(),
						"processID", pid.String(),
						"voteID", b.VoteID.String(),
						"address", b.Address.String(),
					)
					if err := stg.ReleaseVerifiedBallotReservations([][]byte{keys[i]}); err != nil {
						log.Warnw("failed to release ballot reservation after failure marking",
							"error", err.Error(),
							"processID", pid.String(),
							"voteID", b.VoteID.String(),
							"address", b.Address.String(),
						)
					}
				}
				continue
			}
		}

		batchIdx := len(aggBallots)
		if batchIdx >= params.VotesPerBatch {
			remainingKeys := keys[i:]
			if err := stg.ReleaseVerifiedBallotReservations(remainingKeys); err != nil {
				log.Warnw("failed to release ballot reservations", "error", err.Error())
			}
			break
		}

		// Transform the proof into the required format
		var err error
		proofs[batchIdx], err = proofToRecursion(groth16.Proof(b.Proof))
		if err != nil {
			log.Warnw("failed to transform proof for recursion; marking ballot as failed",
				"processID", pid.String(),
				"voteID", b.VoteID.String(),
				"address", b.Address.String(),
				"error", err.Error(),
			)
			if err := stg.MarkVerifiedBallotsFailed(keys[i]); err != nil {
				log.Warnw("failed to mark ballot as failed",
					"error", err.Error(),
					"processID", pid.String(),
					"voteID", b.VoteID.String(),
					"address", b.Address.String(),
				)
				if err := stg.ReleaseVerifiedBallotReservations([][]byte{keys[i]}); err != nil {
					log.Warnw("failed to release ballot reservation after failure marking",
						"error", err.Error(),
						"processID", pid.String(),
						"voteID", b.VoteID.String(),
						"address", b.Address.String(),
					)
				}
			}
			continue
		}

		// Transform and collect the input hash for the proof
		proofsInputHash[batchIdx] = emulated.ValueOf[sw_bn254.ScalarField](b.InputsHash)
		proofsInputsHashInputs = append(proofsInputsHashInputs, b.InputsHash)

		// Prepare the aggregator ballot entry
		aggBallots = append(aggBallots, &storage.AggregatorBallot{
			VoteID:          b.VoteID,
			Address:         b.Address,
			Weight:          b.VoterWeight,
			EncryptedBallot: b.EncryptedBallot,
			CensusProof:     b.CensusProof,
		})
		verifiedBallots = append(verifiedBallots, b)
		processedKeys = append(processedKeys, keys[i])

		// If we've reached the batch size, stop processing more ballots
		if len(aggBallots) >= params.VotesPerBatch {
			remainingKeys := keys[i+1:]
			// Release reservations for any remaining ballots that were not processed
			if err := stg.ReleaseVerifiedBallotReservations(remainingKeys); err != nil {
				log.Warnw("failed to release ballot reservations", "error", err.Error())
			}
			break // We have enough ballots for one batch
		}
	}

	return &aggregationBatchInputs{
		proofs:                 proofs,
		proofsInputHash:        proofsInputHash,
		aggBallots:             aggBallots,
		verifiedBallots:        verifiedBallots,
		processedKeys:          processedKeys,
		proofsInputsHashInputs: proofsInputsHashInputs,
	}, nil
}

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
		if ballotCount >= params.VotesPerBatch {
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
func (s *Sequencer) processAndUpdateBatch(pid types.HexBytes) bool {
	if err := s.aggregateBatch(pid); err != nil {
		log.Warnw("failed to aggregate batch",
			"error", err.Error(),
			"processID", pid.String())
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

	// Ensure the process is accepting votes
	processID := new(types.ProcessID).SetBytes(pid)
	if isAcceptingVotes, err := s.stg.ProcessIsAcceptingVotes(processID); err != nil {
		return fmt.Errorf("failed to check if process is accepting votes: %w", err)
	} else if !isAcceptingVotes {
		return fmt.Errorf("process '%s' is not accepting votes", processID.String())
	}

	// Check if the process has reached max voters
	maxVotersReached, err := s.stg.ProcessMaxVotersReached(processID)
	if err != nil {
		return fmt.Errorf("failed to check if process max voters reached: %w", err)
	}

	// Pull verified ballots from storage
	ballots, keys, err := s.stg.PullVerifiedBallots(pid, params.VotesPerBatch*2) // Pull up to double the batch size, to handle skips
	if err != nil {
		return fmt.Errorf("failed to pull verified ballots: %w", err)
	}

	// If no ballots were pulled, nothing to do
	if len(ballots) == 0 {
		return nil
	}

	// Defensive check: ensure no duplicate addresses in pulled batch
	// This should never happen due to PullVerifiedBallots deduplication,
	// but we add this as a safety net to catch any potential bugs
	addressSeen := make(map[string]bool)
	for _, b := range ballots {
		addr := b.Address.String()
		if addressSeen[addr] {
			return fmt.Errorf("duplicate address in batch of process %s: %s", processID.String(), addr)
		}
		addressSeen[addr] = true
	}

	// Get the current process state to check if the vote ID already exists
	processState, err := s.currentProcessState(new(types.ProcessID).SetBytes(pid))
	if err != nil {
		return fmt.Errorf("failed to get latest process state: %w", err)
	}

	proofToRecursion := func(p groth16.Proof) (stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine], error) {
		return stdgroth16.ValueOfProof[sw_bls12377.G1Affine, sw_bls12377.G2Affine](p)
	}

	verifyOpts := stdgroth16.GetNativeVerifierOptions(
		params.AggregatorCurve.ScalarField(),
		params.VoteVerifierCurve.ScalarField(),
	)
	verifyVoteVerifierProof := func(vb *storage.VerifiedBallot) error {
		if vb == nil {
			return fmt.Errorf("verified ballot is nil")
		}
		if vb.Proof == nil {
			return fmt.Errorf("missing proof")
		}
		if vb.InputsHash == nil {
			return fmt.Errorf("missing inputs hash")
		}
		if s.vvVk == nil {
			return fmt.Errorf("vote verifier verifying key is not loaded")
		}

		inputsHashValue := emulated.ValueOf[sw_bn254.ScalarField](vb.InputsHash)
		pubAssignment := &voteverifier.VerifyVoteCircuit{
			IsValid:    1,
			InputsHash: inputsHashValue,
		}
		pubWitness, err := frontend.NewWitness(pubAssignment, params.VoteVerifierCurve.ScalarField(), frontend.PublicOnly())
		if err != nil {
			return fmt.Errorf("build public witness: %w", err)
		}
		if err := groth16.Verify(vb.Proof, s.vvVk, pubWitness, verifyOpts); err != nil {
			pubAssignmentIsValid0 := &voteverifier.VerifyVoteCircuit{
				IsValid:    0,
				InputsHash: inputsHashValue,
			}
			pubWitnessIsValid0, errIsValid0 := frontend.NewWitness(pubAssignmentIsValid0, params.VoteVerifierCurve.ScalarField(), frontend.PublicOnly())
			if errIsValid0 == nil {
				if err2 := groth16.Verify(vb.Proof, s.vvVk, pubWitnessIsValid0, verifyOpts); err2 == nil {
					return fmt.Errorf("proof verifies only with IsValid=0")
				}
			}
			return fmt.Errorf("verify proof: %w", err)
		}
		return nil
	}

	batchInputs, err := collectAggregationBatchInputs(
		s.stg,
		pid,
		ballots,
		keys,
		processState,
		maxVotersReached,
		proofToRecursion,
		verifyVoteVerifierProof,
	)
	if err != nil {
		return err
	}

	// Check if we have some ballots to process
	if len(batchInputs.aggBallots) == 0 {
		log.Debugw("no ballots to process", "processID", fmt.Sprintf("%x", pid))
		return nil
	}

	log.Debugw("aggregating ballots",
		"processID", pid.String(),
		"ballotCount", len(batchInputs.aggBallots))
	startTime := time.Now()

	// Padding the proofsInputsHashInputs with 1s to fill the array
	for i := len(batchInputs.aggBallots); i < params.VotesPerBatch; i++ {
		batchInputs.proofsInputsHashInputs = append(batchInputs.proofsInputsHashInputs, new(big.Int).SetInt64(1))
	}

	// Compute the hash of the ballot input hashes using MiMC hash function
	inputsHash, err := mimc7.Hash(batchInputs.proofsInputsHashInputs, nil)
	if err != nil {
		return fmt.Errorf("failed to calculate inputs hash: %w", err)
	}

	// Create the aggregator circuit assignment
	assignment := &aggregator.AggregatorCircuit{
		ValidProofs:        len(batchInputs.aggBallots),
		InputsHash:         emulated.ValueOf[sw_bn254.ScalarField](inputsHash),
		Proofs:             batchInputs.proofs,
		ProofsInputsHashes: batchInputs.proofsInputHash,
	}

	// Fill any remaining slots with dummy proofs if needed
	if len(batchInputs.aggBallots) < params.VotesPerBatch {
		log.Debugw("filling with dummy proofs", "count", params.VotesPerBatch-len(batchInputs.aggBallots))
		if err := assignment.FillWithDummy(s.vvCcs, s.vvPk, s.bVkCircom, len(batchInputs.aggBallots), s.prover); err != nil {
			if err := s.stg.ReleaseVerifiedBallotReservations(batchInputs.processedKeys); err != nil {
				log.Warnw("failed to release ballot reservations after dummy fill failure",
					"error", err.Error(),
					"processID", pid.String(),
				)
			}
			return fmt.Errorf("failed to fill with dummy proofs: %w", err)
		}
	}

	// Prepare the circuit assignment
	log.Debugw("inputs ready for aggregation", "took", time.Since(startTime).String())
	startTime = time.Now()

	// Prepare the options for the prover
	opts := stdgroth16.GetNativeProverOptions(
		params.StateTransitionCurve.ScalarField(),
		params.AggregatorCurve.ScalarField(),
	)
	// Generate the proof for the aggregator circuit
	proof, err := s.prover(params.AggregatorCurve, s.aggCcs, s.aggPk, assignment, opts)
	if err != nil {
		// Log detailed debug information about the failure
		// Remove block once we have sufficient confidence in the aggregator proving
		s.debugAggregationFailure(pid, assignment, batchInputs, inputsHash, err)
		var invalidKeys [][]byte
		for i, vb := range batchInputs.verifiedBallots {
			if errVerify := verifyVoteVerifierProof(vb); errVerify != nil {
				invalidKeys = append(invalidKeys, batchInputs.processedKeys[i])
				voteIDStr := "<nil>"
				addressStr := "<nil>"
				if vb != nil {
					if vb.VoteID != nil {
						voteIDStr = vb.VoteID.String()
					}
					if vb.Address != nil {
						addressStr = vb.Address.String()
					}
				}
				log.Warnw("vote verifier proof does not verify for aggregation batch; excluding ballot",
					"processID", pid.String(),
					"voteID", voteIDStr,
					"address", addressStr,
					"error", errVerify.Error(),
				)
			}
		}
		// End debug block
		// Mark any invalid ballots as failed
		if len(invalidKeys) > 0 {
			if err := s.stg.MarkVerifiedBallotsFailed(invalidKeys...); err != nil {
				log.Warnw("failed to mark invalid ballots as failed after aggregation proving failure",
					"error", err.Error(),
					"processID", pid.String(),
					"invalidCount", len(invalidKeys),
				)
			}
		}
		if err := s.stg.ReleaseVerifiedBallotReservations(batchInputs.processedKeys); err != nil {
			log.Warnw("failed to release ballot reservations after aggregation proving failure",
				"error", err.Error(),
				"processID", pid.String(),
			)
		}
		return fmt.Errorf("failed to generate aggregate proof: %w", err)
	}

	log.Infow("aggregate proof generated",
		"took", time.Since(startTime).String(),
		"processID", pid.String(),
		"ballots", len(batchInputs.aggBallots))

	proofBW6, ok := proof.(*groth16_bw6761.Proof)
	if !ok {
		if err := s.stg.ReleaseVerifiedBallotReservations(batchInputs.processedKeys); err != nil {
			log.Warnw("failed to release ballot reservations after unexpected aggregate proof type",
				"error", err.Error(),
				"processID", pid.String(),
			)
		}
		return fmt.Errorf("unexpected aggregate proof type: %T", proof)
	}

	// Store the aggregated batch
	abb := storage.AggregatorBallotBatch{
		ProcessID: pid,
		Proof:     proofBW6,
		Ballots:   batchInputs.aggBallots,
	}

	if err := s.stg.PushAggregatorBatch(&abb); err != nil {
		if err := s.stg.ReleaseVerifiedBallotReservations(batchInputs.processedKeys); err != nil {
			log.Warnw("failed to release ballot reservations after batch push failure",
				"error", err.Error(),
				"processID", pid.String(),
			)
		}
		return fmt.Errorf("failed to push ballot batch: %w", err)
	}

	// Mark the individual ballots as processed
	if err := s.stg.MarkVerifiedBallotsDone(batchInputs.processedKeys...); err != nil {
		if err := s.stg.MarkVerifiedBallotsFailed(batchInputs.processedKeys...); err != nil {
			log.Warnw("failed to mark ballot batch as failed",
				"error", err.Error(),
				"processID", pid.String())
		}
		return fmt.Errorf("failed to mark verified ballots as done: %w", err)
	}
	return nil
}
