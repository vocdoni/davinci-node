package sequencer

import (
	"fmt"
	"maps"
	"time"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	groth16_bn254 "github.com/consensys/gnark/backend/groth16/bn254"
	"github.com/consensys/gnark/backend/solidity"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/statetransition"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/state"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
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
	// copy pids to avoid locking the map for too long
	s.pidsLock.RLock()
	pids := make(map[string]time.Time, len(s.pids))
	maps.Copy(pids, s.pids)
	s.pidsLock.RUnlock()
	// iterate over the process IDs and process the ones that are ready
	for pid := range pids {
		// check if there is a batch ready for processing
		batch, batchID, err := s.stg.NextBallotBatch([]byte(pid))
		if err != nil {
			if err != storage.ErrNoMoreElements {
				log.Errorw(err, "failed to get next ballot batch")
			}
			continue
		}
		// if the batch is nil, skip it
		if batch == nil || len(batch.Ballots) == 0 {
			log.Debugw("no ballots in batch", "batchID", batchID)
			continue
		}
		// decode process ID and load metadata
		processID := new(types.ProcessID).SetBytes(batch.ProcessID)
		log.Debugw("state transition ready for processing",
			"processID", processID.String(),
			"ballotCount", len(batch.Ballots))
		startTime := time.Now()
		// process the batch to get the proof
		proof, pubWitness, err := s.processStateTransitionBatch(processID, batch)
		if err != nil {
			log.Errorw(err, "failed to process state transition batch")
			continue
		}
		log.Debugw("state transition proof generated", "took", time.Since(startTime).String())
		// store the proof in the state transition storage
		if err := s.stg.PushStateTransitionBatch(&storage.StateTransitionBatch{
			ProcessID:  batch.ProcessID,
			Proof:      proof.(*groth16_bn254.Proof),
			PubWitness: pubWitness,
			Ballots:    batch.Ballots,
		}); err != nil {
			log.Errorw(err, "failed to push state transition batch")
			continue
		}
		// mark the batch as done
		if err := s.stg.MarkBallotBatchDone(batchID); err != nil {
			log.Errorw(err, "failed to mark ballot batch as done")
			continue
		}
		// update the last update time
		s.pidsLock.Lock()
		s.pids[pid] = time.Now()
		s.pidsLock.Unlock()
	}
}

func (s *Sequencer) processStateTransitionBatch(
	processID *types.ProcessID,
	batch *storage.AggregatorBallotBatch,
) (groth16.Proof, []byte, error) {
	// initialize the process state
	processState, err := s.loadState(processID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create state: %w", err)
	}
	// generate the state transition assignments from the batch
	assignments, err := s.stateBatchToWitness(processState, batch)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate assignments: %w", err)
	}
	// generate the state transition witness
	witness, err := frontend.NewWitness(assignments, circuits.StateTransitionCurve.ScalarField())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate witness: %w", err)
	}
	// generate the proof with the opt for solidity verifier
	proof, err := groth16.Prove(s.statetransitionCcs, s.statetransitionProvingKey,
		witness, solidity.WithProverTargetSolidityVerifier(backend.GROTH16))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate proof: %w", err)
	}

	pubWitness, err := witness.Public()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to extract public witness: %w", err)
	}
	schema, err := frontend.NewSchema(assignments)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create schema: %w", err)
	}
	pubWitnessJSON, err := pubWitness.ToJSON(schema)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert public witness to JSON: %w", err)
	}
	return proof, pubWitnessJSON, nil
}

func (s *Sequencer) loadState(pid *types.ProcessID) (*state.State, error) {
	// initialize the process state
	ffPID := crypto.BigToFF(circuits.StateTransitionCurve.BaseField(), pid.BigInt())
	processState, err := state.New(s.stg.StateDB(), ffPID)
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
			Ballot:     &v.EncryptedBallot,
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
