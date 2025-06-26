package sequencer

import (
	"fmt"
	"time"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	groth16_bn254 "github.com/consensys/gnark/backend/groth16/bn254"
	"github.com/consensys/gnark/backend/solidity"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

func (s *Sequencer) startStateTransitionProcessor() error {
	const tickInterval = time.Second
	ticker := time.NewTicker(tickInterval)

	go func() {
		defer ticker.Stop()
		log.Infow("state transition processor started",
			"tickInterval", tickInterval)

		for {
			select {
			case <-s.ctx.Done():
				log.Infow("state transition processor stopped")
				return
			case <-ticker.C:
				s.processPendingTransitions()
			}
		}
	}()
	return nil
}

func (s *Sequencer) processPendingTransitions() {
	// process each registered process ID
	s.pids.ForEach(func(pid []byte, _ time.Time) bool {
		// check if there is a batch ready for processing
		batch, batchID, err := s.stg.NextBallotBatch(pid)
		if err != nil {
			if err != storage.ErrNoMoreElements {
				log.Errorw(err, "failed to get next ballot batch")
			}
			return true // Continue to next process ID
		}
		// if the batch is nil, skip it
		if batch == nil || len(batch.Ballots) == 0 {
			log.Debugw("no ballots in batch", "batchID", batchID)
			return true // Continue to next process ID
		}
		// decode process ID and load metadata
		processID := new(types.ProcessID).SetBytes(batch.ProcessID)

		// lock the processor to avoid concurrent workloads
		s.workInProgressLock.Lock()
		defer s.workInProgressLock.Unlock()
		startTime := time.Now()

		// initialize the process state
		processState, err := s.latestProcessState(processID)
		if err != nil {
			log.Errorw(err, "failed to load process state")
			return true // Continue to next process ID
		}

		// get the root hash, this is the state before the batch
		root, err := processState.RootAsBigInt()
		if err != nil {
			log.Errorw(err, "failed to get root")
			return true // Continue to next process ID
		}

		log.Debugw("state transition ready for processing",
			"processID", processID.String(),
			"ballotCount", len(batch.Ballots),
			"rootHashBefore", root.String(),
		)

		// process the batch to get the proof
		proof, err := s.processStateTransitionBatch(processState, batch)
		if err != nil {
			log.Errorw(err, "failed to process state transition batch")
			if err := s.stg.MarkBallotBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		// Get raw public inputs
		rootHashAfter, err := processState.RootAsBigInt()
		if err != nil {
			log.Errorw(err, "failed to get root hash after")
			if err := s.stg.MarkBallotBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		log.Infow("state transition proof generated",
			"took", time.Since(startTime).String(),
			"pid", processID.String(),
			"rootHashBefore", root.String(),
			"rootHashAfter", rootHashAfter.String(),
		)

		// Store the proof in the state transition storage
		if err := s.stg.PushStateTransitionBatch(&storage.StateTransitionBatch{
			ProcessID: batch.ProcessID,
			Proof:     proof.(*groth16_bn254.Proof),
			Ballots:   batch.Ballots,
			Inputs: storage.StateTransitionBatchProofInputs{
				RootHashBefore: processState.RootHashBefore(),
				RootHashAfter:  rootHashAfter,
				NumNewVotes:    processState.BallotCount(),
				NumOverwritten: processState.OverwrittenCount(),
			},
		}); err != nil {
			log.Errorw(err, "failed to push state transition batch")
			if err := s.stg.MarkBallotBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		// Mark the batch as done
		if err := s.stg.MarkBallotBatchDone(batchID); err != nil {
			log.Errorw(err, "failed to mark ballot batch as done")
			return true // Continue to next process ID
		}
		// Update the last update time by re-adding the process ID
		s.pids.Add(pid) // This will update the timestamp
		return true     // Continue to next process ID
	})
}

func (s *Sequencer) processStateTransitionBatch(
	processState *state.State,
	batch *storage.AggregatorBallotBatch,
) (groth16.Proof, error) {
	startTime := time.Now()
	// generate the state transition assignments from the batch
	assignments, err := s.stateBatchToWitness(processState, batch)
	if err != nil {
		return nil, fmt.Errorf("failed to generate assignments: %w", err)
	}
	log.Debugw("state transition assignments ready for proof generation", "took", time.Since(startTime).String())

	// Prepare the options for the prover - use solidity verifier target
	opts := solidity.WithProverTargetSolidityVerifier(backend.GROTH16)

	// Generate the proof
	proof, err := s.prover(
		circuits.StateTransitionCurve,
		s.stCcs,
		s.stPk,
		assignments,
		opts,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate proof: %w", err)
	}
	return proof, nil
}

func (s *Sequencer) latestProcessState(pid *types.ProcessID) (*state.State, error) {
	// get the process from the storage
	process, err := s.stg.Process(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to get process metadata: %w", err)
	}
	// initialize the process state on the given root
	processState, err := state.LoadOnRoot(s.stg.StateDB(), pid.BigInt(), process.StateRoot.MathBigInt())
	if err != nil {
		return nil, fmt.Errorf("failed to create state: %w", err)
	}
	return processState, nil
}

func (s *Sequencer) stateBatchToWitness(
	processState *state.State,
	batch *storage.AggregatorBallotBatch,
) (*statetransition.StateTransitionCircuit, error) {
	// start a new batch
	if err := processState.StartBatch(); err != nil {
		return nil, fmt.Errorf("failed to start batch: %w", err)
	}
	// add the new ballots to the state
	for _, v := range batch.Ballots {
		if err := processState.AddVote(&state.Vote{
			Nullifier:  v.Nullifier,
			Ballot:     v.EncryptedBallot,
			Address:    v.Address,
			Commitment: v.Commitment,
		}); err != nil {
			return nil, fmt.Errorf("failed to add vote: %w", err)
		}
	}
	// end the batch
	if err := processState.EndBatch(); err != nil {
		return nil, fmt.Errorf("failed to end batch: %w", err)
	}
	// generate the state transition witness
	proofWitness, err := statetransition.GenerateWitness(processState)
	if err != nil {
		return nil, fmt.Errorf("failed to generate witness: %w", err)
	}
	proofWitness.AggregatorProof, err = stdgroth16.ValueOfProof[sw_bw6761.G1Affine, sw_bw6761.G2Affine](batch.Proof)
	if err != nil {
		return nil, fmt.Errorf("failed to transform recursive proof: %w", err)
	}
	return proofWitness, nil
}
