package sequencer

import (
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/solidity"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

const (
	transitionOnChainTickInterval = 10 * time.Second
	resultsOnChainTickInterval    = 10 * time.Second
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
		log.Infow("state transition batch ready for on-chain upload",
			"pid", hex.EncodeToString(pid),
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
		// update the process state with the new root hash and the vote counts atomically
		if err := s.stg.UpdateProcess(pid, func(p *types.Process) error {
			p.StateRoot = (*types.BigInt)(batch.Inputs.RootHashAfter)
			p.VoteCount = new(types.BigInt).Add(p.VoteCount, new(types.BigInt).SetInt(batch.Inputs.NumNewVotes))
			p.VoteOverwriteCount = new(types.BigInt).Add(p.VoteOverwriteCount, new(types.BigInt).SetInt(batch.Inputs.NumOverwrites))
			return nil
		}); err != nil {
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
	for {
		res, err := s.stg.NextVerifiedResults()
		if err != nil {
			if !errors.Is(err, storage.ErrNoMoreElements) {
				log.Errorw(err, "failed to pull verified results")
			}
			break
		}
		// Transform the gnark proof to a solidity proof and upload it to the
		// contract.
		solidityProof := new(solidity.Groth16CommitmentProof)
		if err := solidityProof.FromGnarkProof(res.Proof); err != nil {
			log.Errorw(err, "failed to convert gnark proof to solidity proof")
			continue // Continue to next process ID
		}
		abiProof, err := solidityProof.ABIEncode()
		if err != nil {
			log.Errorw(err, "failed to encode proof for contract upload")
			continue // Continue to next process ID
		}
		abiInputs, err := res.Inputs.ABIEncode()
		if err != nil {
			log.Errorw(err, "failed to encode inputs for contract upload")
			continue // Continue to next process ID
		}
		log.Debugw("verified results ready to upload to contract", "pid", res.ProcessID.String())
		// Simulate tx to the contract to check if it will fail and get the root
		// cause of the failure if it does
		var pid32 [32]byte
		copy(pid32[:], res.ProcessID)
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
				"pid", res.ProcessID.String())
		}
		// submit the proof to the contract
		txHash, err := s.contracts.SetProcessResults(res.ProcessID, abiProof, abiInputs)
		if err != nil {
			log.Errorw(err, "failed to upload verified results to contract")
			continue // Continue to next process ID
		}
		// wait for the transaction to be mined
		if err := s.contracts.WaitTx(*txHash, time.Second*120); err != nil {
			log.Errorw(err, "failed to wait for verified results upload transaction")
			continue // Continue to next process ID
		}
		log.Infow("verified results uploaded to contract",
			"pid", hex.EncodeToString(res.ProcessID),
			"txHash", txHash.String(),
			"results", res.Inputs.Results)

		if err := s.stg.MarkVerifiedResultsDone(res.ProcessID); err != nil {
			log.Errorw(err, "failed to mark verified results as uploaded")
			continue // Continue to next process ID
		}
	}
}
