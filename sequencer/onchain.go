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
	resultsOnChainTickInterval    = 5 * time.Second
)

func (s *Sequencer) startOnchainProcessor() error {
	// Create tickers for processing on-chain transitions and listening for
	// finished processes
	transitionTicker := time.NewTicker(transitionOnChainTickInterval)
	resultsTicker := time.NewTicker(resultsOnChainTickInterval)
	// Run a loop in a goroutine to handle the on-chain processing and
	// finished processes
	go func() {
		defer transitionTicker.Stop()
		log.Infow("on-chain processor started",
			"transitionOnChainInterval", transitionOnChainTickInterval,
			"resultsOnChainInterval", resultsOnChainTickInterval)

		for {
			select {
			case <-s.ctx.Done():
				log.Infow("on-chain processor stopped")
				return
			case <-transitionTicker.C:
				s.processTransitionOnChain()
			case <-resultsTicker.C:
				s.processResultsOnChain()
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
		// update the process state with the new root hash and the vote counts
		process.StateRoot = (*types.BigInt)(batch.Inputs.RootHashAfter)
		process.VoteCount = new(types.BigInt).Add(process.VoteCount, new(types.BigInt).SetInt(batch.Inputs.NumNewVotes))
		process.VoteOverwriteCount = new(types.BigInt).Add(process.VoteOverwriteCount, new(types.BigInt).SetInt(batch.Inputs.NumOverwrites))
		if err := s.stg.SetProcess(process); err != nil {
			log.Errorw(err, "failed to update process data")
			return true // Continue to next process ID
		}
		log.Infow("process state root updated",
			"pid", hex.EncodeToString(pid),
			"rootHashBefore", batch.Inputs.RootHashBefore.String(),
			"rootHashAfter", batch.Inputs.RootHashAfter.String())
		// mark the batch as done
		if err := s.stg.MarkStateTransitionBatchDone(batchID, pid); err != nil {
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
	log.Debugw("proof ready to submit to the contract", "pid", hex.EncodeToString(processID))

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

	// Submit the proof to the contract if simulation is successful
	txHash, err := s.contracts.SetProcessTransition(processID,
		abiProof,
		abiInputs,
		(*types.BigInt)(inputs.RootHashBefore),
	)
	if err != nil {
		return fmt.Errorf("failed to submit state transition: %w", err)
	}

	// Wait for the transaction to be mined
	return s.contracts.WaitTx(*txHash, time.Second*60)
}

func (s *Sequencer) processResultsOnChain() {
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
		return true
	})
}
