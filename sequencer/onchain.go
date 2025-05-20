package sequencer

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/solidity"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

func (s *Sequencer) startOnchainProcessor() error {
	const tickInterval = 5 * time.Second
	ticker := time.NewTicker(tickInterval)

	go func() {
		defer ticker.Stop()
		log.Infow("on-chain processor started",
			"tickInterval", tickInterval)

		for {
			select {
			case <-s.ctx.Done():
				log.Infow("on-chain processor stopped")
				return
			case <-ticker.C:
				s.processOnChain()
			}
		}
	}()
	return nil
}

func (s *Sequencer) processOnChain() {
	// process each registered process ID
	s.pids.ForEach(func(pid []byte, _ time.Time) bool {
		// get a batch ready for uploading on-chain
		batch, batchID, err := s.stg.NextStateTransitionBatch(pid)
		if err != nil {
			if err != storage.ErrNoMoreElements {
				log.Errorw(err, "failed to get next state transition batch")
			}
			return true // Continue to next process ID
		}
		// get the process from the storage
		process, err := s.stg.Process(new(types.ProcessID).SetBytes(pid))
		if err != nil {
			log.Errorw(err, "failed to get process data")
			return true // Continue to next process ID
		}
		log.Infow("state transition batch ready for on-chain upload",
			"pid", hex.EncodeToString([]byte(pid)),
			"batchID", hex.EncodeToString(batchID))
		// convert the gnark proof to a solidity proof
		solidityCommitmentProof := new(solidity.Groth16CommitmentProof)
		if err := solidityCommitmentProof.FromGnarkProof(batch.Proof); err != nil {
			log.Errorw(err, "failed to convert gnark proof to solidity proof")
			return true // Continue to next process ID
		}
		// send the proof to the contract with the public witness
		if err := s.pushToContract([]byte(pid), solidityCommitmentProof, batch.Inputs); err != nil {
			log.Errorw(err, "failed to push to contract")
			return true // Continue to next process ID
		}
		// update the process state with the new root hash
		process.StateRoot = (*types.BigInt)(batch.Inputs.RootHashAfter)
		if err := s.stg.SetProcess(process); err != nil {
			log.Errorw(err, "failed to update process data")
			return true // Continue to next process ID
		}
		log.Infow("process state root updated",
			"pid", hex.EncodeToString([]byte(pid)),
			"rootHashBefore", batch.Inputs.RootHashBefore.String(),
			"rootHashAfter", batch.Inputs.RootHashAfter.String())
		// mark the batch as done
		if err := s.stg.MarkStateTransitionBatchDone(batchID); err != nil {
			log.Errorw(err, "failed to mark state transition batch as done")
			return true // Continue to next process ID
		}
		// update the last update time by re-adding the process ID
		s.pids.Add(pid) // This will update the timestamp

		return true // Continue to next process ID
	})
}

func (s *Sequencer) pushToContract(processID []byte,
	proof *solidity.Groth16CommitmentProof,
	inputs storage.StateTransitionBatchProofInputs,
) error {
	// convert the proof to a solidity proof
	abiProof, err := proof.ABIEncode()
	if err != nil {
		return fmt.Errorf("failed to encode proof: %w", err)
	}
	abiInputs, err := inputs.ABIEncode()
	if err != nil {
		return fmt.Errorf("failed to encode inputs: %w", err)
	}
	log.Debugw("proof ready to submit to the contract",
		"commitments", proof.Commitments,
		"commitmentPok", proof.CommitmentPok,
		"proof", proof.Proof,
		"abiProof", hex.EncodeToString(abiProof),
		"inputs", inputs,
		"abiInputs", hex.EncodeToString(abiInputs),
	)
	// submit the proof to the contract
	txHash, err := s.contracts.SetProcessTransition(processID,
		abiProof,
		abiInputs,
		(*types.BigInt)(inputs.RootHashBefore),
	)
	if err != nil {
		return fmt.Errorf("failed to submit state transition: %w", err)
	}
	// wait for the transaction to be mined
	return s.contracts.WaitTx(*txHash, time.Second*60)
}
