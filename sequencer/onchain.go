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

const (
	transitionOnChainTickInterval = 5 * time.Second
	finishedListenerTickInterval  = 10 * time.Second
	resultsOnChainTickInterval    = 5 * time.Second
)

func (s *Sequencer) startOnchainProcessor() error {
	// Create tickers for processing on-chain transitions and listening for
	// finished processes
	transitionTicker := time.NewTicker(transitionOnChainTickInterval)
	finishedTicker := time.NewTicker(finishedListenerTickInterval)
	resultsTicker := time.NewTicker(resultsOnChainTickInterval)
	// Run a loop in a goroutine to handle the on-chain processing and
	// finished processes
	go func() {
		defer transitionTicker.Stop()
		log.Infow("on-chain processor started",
			"transitionOnChainInterval", transitionOnChainTickInterval,
			"finishedListenerInterval", finishedListenerTickInterval,
			"resultsOnChainInterval", resultsOnChainTickInterval)

		for {
			select {
			case <-s.ctx.Done():
				log.Infow("on-chain processor stopped")
				return
			case <-transitionTicker.C:
				s.processTransitionOnChain()
			case <-finishedTicker.C:
				s.listenFinishedProcesses()
			case <-resultsTicker.C:
				s.uploadVerifiedResultsToContract()
			}
		}
	}()
	return nil
}

func (s *Sequencer) processTransitionOnChain() {
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
		if err := s.pushTransitionToContract([]byte(pid), solidityCommitmentProof, batch.Inputs); err != nil {
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

func (s *Sequencer) pushTransitionToContract(processID []byte,
	proof *solidity.Groth16CommitmentProof,
	inputs storage.StateTransitionBatchProofInputs,
) error {
	var pid32 [32]byte
	copy(pid32[:], processID)
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
		"pid", hex.EncodeToString(processID),
		"commitments", proof.Commitments,
		"commitmentPok", proof.CommitmentPok,
		"proof", proof.Proof,
		"abiProof", hex.EncodeToString(abiProof),
		"inputs", inputs,
		"abiInputs", hex.EncodeToString(abiInputs),
	)

	// Simulate tx to the contract to check if it will fail and get the root
	// cause of the failure if it does
	if err := s.contracts.SimulateContractCall(
		s.ctx,
		s.contracts.ContractsAddresses.ProcessRegistry,
		s.contracts.ContractABIs.ProcessRegistry,
		"submitStateTransition",
		pid32,
		abiProof,
		abiInputs,
	); err != nil {
		log.Warnw("failed to simulate state transition",
			"error", err,
			"pid", hex.EncodeToString(processID))
	}

	// submit the proof to the contract if simulation is successful
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

func (s *Sequencer) listenFinishedProcesses() {
	// process each registered process ID
	s.pids.ForEach(func(pid []byte, _ time.Time) bool {
		// Check if the process has encryption keys, if not, skip it
		processID := new(types.ProcessID).SetBytes(pid)
		pubKey, privKey, err := s.stg.EncryptionKeys(processID)
		if err != nil || pubKey == nil || privKey == nil {
			return true // Continue to next process ID
		}
		// Get the process data from the contract
		process, err := s.contracts.Process(pid)
		if err != nil {
			log.Errorw(err, "failed to get process data from contract")
			return true // Continue to next process ID
		}
		// If the process is not ended, skip it
		if process.Status != types.ProcessStatusEnded {
			return true // Continue to next process ID
		}
		// If the process is already verifying results, skip it
		if s.stg.IsVerifyingProcessResults(pid) {
			log.Debugw("ended process is already verifying results, skipping",
				"pid", hex.EncodeToString(pid))
			return true // Continue to next process ID
		}
		// If the process has ended, send it to the finalizer to compute the
		// final results and remove it from the list to avoid reprocessing
		log.Infow("process ready to finalize",
			"pid", hex.EncodeToString(pid),
			"status", process.Status)
		// Send the process ID to the finalizer
		s.OndemandCh <- new(types.ProcessID).SetBytes(pid)
		return true // Continue to next process ID
	})
}

func (s *Sequencer) uploadVerifiedResultsToContract() {
	// process each registered process ID
	s.pids.ForEach(func(pid []byte, _ time.Time) bool {
		res, err := s.stg.PullVerifiedResults(pid)
		if err != nil {
			if err != storage.ErrNoMoreElements {
				log.Errorw(err, "failed to pull verified results")
			}
			return true // Continue to next process ID
		}
		// Transform the gnark proof to a solidity proof and upload it to the
		// contract.
		solidityProof := new(solidity.Groth16CommitmentProof)
		if err := solidityProof.FromGnarkProof(res.Proof); err != nil {
			log.Errorw(err, "failed to convert gnark proof to solidity proof")
			return true // Continue to next process ID
		}
		abiProof, err := solidityProof.ABIEncode()
		if err != nil {
			log.Errorw(err, "failed to encode proof for contract upload")
			return true // Continue to next process ID
		}
		abiInputs, err := res.Inputs.ABIEncode()
		if err != nil {
			log.Errorw(err, "failed to encode inputs for contract upload")
			return true // Continue to next process ID
		}
		log.Infow("verified results ready to upload to contract", "pid", hex.EncodeToString(pid))
		// Simulate tx to the contract to check if it will fail and get the root
		// cause of the failure if it does
		var pid32 [32]byte
		copy(pid32[:], pid)
		if err := s.contracts.SimulateContractCall(
			s.ctx,
			s.contracts.ContractsAddresses.ProcessRegistry,
			s.contracts.ContractABIs.ProcessRegistry,
			"setProcessResults",
			pid32,
			abiProof,
			abiInputs,
		); err != nil {
			log.Warnw("failed to simulate verified results upload",
				"error", err,
				"pid", hex.EncodeToString(pid))
		}
		// submit the proof to the contract
		txHash, err := s.contracts.SetProcessResults(pid, abiProof, abiInputs)
		if err != nil {
			log.Errorw(err, "failed to upload verified results to contract")
			return true // Continue to next process ID
		}
		// wait for the transaction to be mined
		if err := s.contracts.WaitTx(*txHash, time.Second*60); err != nil {
			log.Errorw(err, "failed to wait for verified results upload transaction")
			return true // Continue to next process ID
		}
		log.Infow("verified results uploaded to contract",
			"pid", hex.EncodeToString(pid),
			"txHash", txHash.String(),
			"results", res.Inputs.Results)

		if err := s.stg.MarkVerifiedResults(pid); err != nil {
			log.Errorw(err, "failed to mark verified results as uploaded")
			return true // Continue to next process ID
		}

		s.pids.Remove(pid) // Remove the process ID from the list after uploading results

		return true
	})
}
