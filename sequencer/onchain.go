package sequencer

import (
	"errors"
	"fmt"
	"time"

	gethkzg "github.com/ethereum/go-ethereum/crypto/kzg4844"

	ethereumtypes "github.com/ethereum/go-ethereum/core/types"
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
		if err := s.pushTransitionToContract([]byte(pid), solidityCommitmentProof, batch.Inputs, batch.BlobSidecar); err != nil {
			log.Errorw(err, "failed to push to contract")
			return true // Continue to next process ID
		}
		// update the process state with the new root hash and the vote counts atomically
		if err := s.stg.UpdateProcess(pid, func(p *types.Process) error {
			p.StateRoot = (*types.BigInt)(batch.Inputs.RootHashAfter)
			p.VoteCount = new(types.BigInt).Add(p.VoteCount, new(types.BigInt).SetInt(batch.Inputs.NumNewVotes))
			p.VoteOverwrittenCount = new(types.BigInt).Add(p.VoteOverwrittenCount, new(types.BigInt).SetInt(batch.Inputs.NumOverwritten))
			return nil
		}); err != nil {
			log.Errorw(err, "failed to update process data")
			return true // Continue to next process ID
		}
		log.Infow("process state root updated",
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
	proof *solidity.Groth16CommitmentProof,
	inputs storage.StateTransitionBatchProofInputs,
	blobSidecar *ethereumtypes.BlobTxSidecar,
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

	// Verify the blob proof locally before sending to the contract
	if err := gethkzg.VerifyBlobProof(&blobSidecar.Blobs[0], blobSidecar.Commitments[0], blobSidecar.Proofs[0]); err != nil {
		return fmt.Errorf("local blob proof verification failed: %w", err)
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
	callback := s.pushStateTransitionCallback(processID)
	if err := s.contracts.SetProcessTransition(
		processID,
		abiProof,
		abiInputs,
		blobSidecar,
		transitionOnChainTimeout,
		callback,
	); err != nil {
		// TODO: mark the batch as failed? recover the pending batch?
		// If this function returns an error, the caller skips to the next
		// pending transition, so it should be retried later.
		return fmt.Errorf("failed to set process transition: %w", err)
	}
	return nil
}

// recoverPendingBatch attempts to recover any pending state
// transitions for the given process ID by re-pushing the batch to storage to
// be retried later in a new state transition cycle.
func (s *Sequencer) recoverPendingBatch(pid types.HexBytes) error {
	pendingBatch, err := s.stg.PendingAggregatorBatch(pid)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("failed to get pending aggregator batch: %w", err)
	}
	if pendingBatch != nil {
		if err := s.stg.PushAggregatorBatch(pendingBatch); err != nil {
			return fmt.Errorf("failed to recover pending aggregator batch: %w", err)
		}
	}
	return nil
}

// pushStateTransitionCallback returns a callback function to be called when
// the state transition transaction is mined or fails. It handles logging and
// recovery of pending state transitions.
func (s *Sequencer) pushStateTransitionCallback(pid types.HexBytes) func(err error) {
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
		// If there was an error, log it and recover the batch if possible
		if err != nil {
			log.Errorf("failed to wait for state transition of %s: %s", pid.String(), err)
			if err := s.recoverPendingBatch(pid); err != nil {
				log.Warnw("failed to recover pending state transition after state transition failure",
					"error", err,
					"processID", pid.String())
			}
			return
		}
		log.Infow("state transition uploaded to contract", "pid", pid.String())
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
