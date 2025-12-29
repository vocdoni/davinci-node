package sequencer

import (
	"errors"
	"fmt"
	"time"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/solidity"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

const (
	// Interval for checking and processing state transitions to be sent on-chain
	transitionOnChainTickInterval = 10 * time.Second
	// Timeout for waiting for a state transition transaction to be mined (not sent)
	transitionOnChainTimeout = 30 * time.Minute
	// Interval for checking and processing verified results to be sent on-chain
	resultsOnChainTickInterval = 10 * time.Second
	// Timeout for waiting for a verified results transaction to be mined (not sent)
	resultsOnChainTimeout = 2 * time.Minute
	// Maximum number of retries for uploading verified results
	maxResultsUploadRetries = 3
)

// startOnchainProcessor starts the on-chain processor that periodically
// processes state transitions and verified results to be uploaded to the
// contract.
func (s *Sequencer) startOnchainProcessor() error {
	// Create tickers for processing on-chain transitions and listening for
	// finished processes
	transitionTicker := time.NewTicker(transitionOnChainTickInterval)
	resultsTicker := time.NewTicker(resultsOnChainTickInterval)
	// Run a loop in a goroutine to handle the on-chain processing and
	// finished processes
	go func() {
		defer transitionTicker.Stop()
		defer resultsTicker.Stop()
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

// processTransitionOnChain processes state transition batches ready to be
// uploaded on-chain for each registered process ID.
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
			"pid", fmt.Sprintf("%x", pid),
			"batchID", fmt.Sprintf("%x", batchID))

		// check the remote state root matches the local one
		remoteStateRoot, err := s.contracts.StateRoot(pid)
		if err != nil || remoteStateRoot == nil {
			log.Errorw(err, "failed to get remote state root for: "+fmt.Sprintf("%x", pid))
			return true // Continue to next process ID
		}
		thisStateRoot := batch.Inputs.RootHashBefore
		if remoteStateRoot.MathBigInt().Cmp(thisStateRoot) != 0 {
			log.Errorw(fmt.Errorf("state root mismatch for processId %s: local %s != remote %s",
				fmt.Sprintf("%x", pid), thisStateRoot.String(), remoteStateRoot.String()), "could not push state transition to contract")
			// Mark the batch as outdated so we don't process it again
			// and a new one will be generated with the correct root
			if err := s.stg.MarkStateTransitionBatchOutdated(batchID); err != nil {
				log.Errorw(err, "failed to mark state transition batch as outdated")
			}
			// TODO: we should probably mark the batch as failed and not retry forever
			return true // Continue to next process ID
		}

		// convert the gnark proof to a solidity proof
		solidityCommitmentProof := new(solidity.Groth16CommitmentProof)
		if err := solidityCommitmentProof.FromGnarkProof(batch.Proof); err != nil {
			log.Errorw(err, "failed to convert gnark proof to solidity proof")
			return true // Continue to next process ID
		}

		// send the proof to the contract with the public witness
		if err := s.pushTransitionToContract(pid, batchID, solidityCommitmentProof, batch.Inputs, batch.BlobSidecar); err != nil {
			log.Errorw(err, "failed to push to contract")
			if err := s.stg.MarkStateTransitionBatchFailed(batchID, pid); err != nil {
				log.Errorw(err, "failed to mark state transition batch as failed")
			}
			return true // Continue to next process ID
		}
		log.Infow("process state transition pushed",
			"pid", fmt.Sprintf("%x", pid),
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

// pushTransitionToContract pushes the given state transition proof and inputs
// to the smart contract for the given process ID.
func (s *Sequencer) pushTransitionToContract(
	processID types.HexBytes,
	batchID []byte,
	proof *solidity.Groth16CommitmentProof,
	inputs storage.StateTransitionBatchProofInputs,
	blobSidecar *types.BlobTxSidecar,
) error {
	var pid32 [32]byte
	copy(pid32[:], processID)

	abiProof, err := proof.ABIEncode()
	if err != nil {
		return fmt.Errorf("failed to encode proof: %w", err)
	}
	abiInputs, err := inputs.ABIEncode()
	if err != nil {
		return fmt.Errorf("failed to encode inputs: %w", err)
	}

	log.Debugw("proof ready to submit to the contract",
		"pid", processID.String(),
		"abiProof", fmt.Sprintf("%x", abiProof),
		"abiInputs", fmt.Sprintf("%x", abiInputs),
		"strProof", proof.String(),
		"strInputs", inputs.String())

	// If the current contracts support blob transactions, verify the sidecar
	// Note: With EIP-7594 (Fusaka), we use Version 1 sidecars with cell proofs.
	// Cell proofs are verified during their generation in ComputeCellsAndKZGProofs.
	if s.contracts.SupportBlobTxs() {
		// Verify sidecar version and structure
		if blobSidecar.Version != types.BlobSidecarVersion1 {
			return fmt.Errorf("unexpected blob sidecar version: got %d, expected %d",
				blobSidecar.Version, types.BlobSidecarVersion1)
		}
		// Verify we have the correct number of cell proofs (128 per blob)
		expectedProofs := len(blobSidecar.Blobs) * types.CellProofsPerBlob
		if len(blobSidecar.Proofs) != expectedProofs {
			return fmt.Errorf("incorrect number of cell proofs: got %d, expected %d",
				len(blobSidecar.Proofs), expectedProofs)
		}
	} else {
		// If they does not support blob transactions, set sidecar to nil to
		// avoid sending it
		blobSidecar = nil
	}

	// Simulate tx to the contract to check if it will fail and get the root
	// cause of the failure if it does
	if err := s.contracts.SimulateContractCall(
		s.ctx,
		s.contracts.ContractsAddresses.ProcessRegistry,
		s.contracts.ContractABIs.ProcessRegistry,
		"submitStateTransition",
		blobSidecar,
		pid32,
		abiProof,
		abiInputs,
	); err != nil {
		log.Debugw("failed to simulate state transition",
			"error", err,
			"pid", processID.String())
	}

	// Submit the proof to the contract
	log.Infow("state transition pending to be mined",
		"pid", processID.String())
	// Create a callback for the state transition
	callback := s.pushStateTransitionCallback(processID, batchID)
	if err := s.contracts.SetProcessTransition(
		processID,
		abiProof,
		abiInputs,
		blobSidecar,
		transitionOnChainTimeout,
		callback,
	); err != nil {
		return fmt.Errorf("failed to set process transition: %w", err)
	}
	return nil
}

// pushStateTransitionCallback returns a callback function to be called when
// the state transition transaction is mined or fails. It handles logging and
// recovery of pending state transitions.
func (s *Sequencer) pushStateTransitionCallback(pid types.HexBytes, batchID []byte) func(err error) {
	return func(err error) {
		defer func() {
			// Remove the pending tx mark
			if err := s.stg.PrunePendingTx(storage.StateTransitionTx, pid); err != nil {
				log.Warnw("failed to release pending tx",
					"error", err,
					"processID", pid.String())
			}
			log.Infow("pending tx released", "pid", pid.String())
		}()
		// If there was an error, log it and mark the batch as failed
		if err != nil {
			log.Errorf("failed to wait for state transition of %s: %s", pid.String(), err)
			// Use MarkStateTransitionBatchFailed for consistent recovery logic
			if err := s.stg.MarkStateTransitionBatchFailed(batchID, pid); err != nil {
				log.Warnw("failed to mark state transition batch as failed after callback error",
					"error", err,
					"processID", pid.String())
			}
			return
		}
		log.Infow("state transition pushed to contract", "pid", pid.String())
	}
}

// processResultsOnChain processes verified results and uploads them to the
// contract.
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
			// Mark as done to avoid getting stuck
			if err := s.stg.MarkVerifiedResultsDone(res.ProcessID); err != nil {
				log.Errorw(err, "failed to mark verified results as done after proof conversion failure")
			}
			continue // Continue to next process ID
		}
		abiProof, err := solidityProof.ABIEncode()
		if err != nil {
			log.Errorw(err, "failed to encode proof for contract upload")
			// Mark as done to avoid getting stuck
			if err := s.stg.MarkVerifiedResultsDone(res.ProcessID); err != nil {
				log.Errorw(err, "failed to mark verified results as done after proof encoding failure")
			}
			continue // Continue to next process ID
		}
		abiInputs, err := res.Inputs.ABIEncode()
		if err != nil {
			log.Errorw(err, "failed to encode inputs for contract upload")
			// Mark as done to avoid getting stuck
			if err := s.stg.MarkVerifiedResultsDone(res.ProcessID); err != nil {
				log.Errorw(err, "failed to mark verified results as done after inputs encoding failure")
			}
			continue // Continue to next process ID
		}
		log.Debugw("verified results ready to upload to contract",
			"pid", res.ProcessID.String(),
			"abiProof", fmt.Sprintf("%x", abiProof),
			"abiInputs", fmt.Sprintf("%x", abiInputs),
			"strProof", solidityProof.String(),
			"strInputs", res.Inputs.String())
		// Simulate tx to the contract to check if it will fail and get the root
		// cause of the failure if it does
		var pid32 [32]byte
		copy(pid32[:], res.ProcessID)
		if err := s.contracts.SimulateContractCall(
			s.ctx,
			s.contracts.ContractsAddresses.ProcessRegistry,
			s.contracts.ContractABIs.ProcessRegistry,
			"setProcessResults",
			nil, // No blob sidecar for regular contract calls
			pid32,
			abiProof,
			abiInputs,
		); err != nil {
			log.Debugw("failed to simulate verified results upload",
				"error", err,
				"pid", res.ProcessID.String())
		}

		// Try to upload with retries
		var uploadSuccess bool
		var lastErr error

		for attempt := range maxResultsUploadRetries {
			if attempt > 0 {
				log.Debugw("retrying verified results upload",
					"attempt", attempt+1,
					"processID", res.ProcessID.String())
				time.Sleep(time.Second * 2) // Simple 2-second delay between retries
			}

			// submit the proof to the contract and wait for the transaction to
			// be mined
			err := s.contracts.SetProcessResults(res.ProcessID, abiProof, abiInputs, resultsOnChainTimeout)
			if err != nil {
				lastErr = err
				log.Warnw("failed to upload verified results",
					"attempt", attempt+1,
					"error", err,
					"processID", res.ProcessID.String())
				continue
			}

			// Success!
			log.Infow("verified results uploaded to contract",
				"pid", res.ProcessID.String(),
				"results", res.Inputs.Results,
				"attempt", attempt+1)
			uploadSuccess = true
			break
		}

		if !uploadSuccess {
			log.Warnw("discarding verified results after failed upload attempts",
				"processID", res.ProcessID.String(),
				"maxRetries", maxResultsUploadRetries,
				"lastError", lastErr.Error())
		}

		// Always mark as done - whether success or failure after max retries
		// This ensures we don't get stuck on the same result forever
		if err := s.stg.MarkVerifiedResultsDone(res.ProcessID); err != nil {
			log.Errorw(err, "failed to mark verified results as done")
			continue // Continue to next process ID
		}
	}
}
