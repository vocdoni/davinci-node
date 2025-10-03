package sequencer

import (
	"encoding/hex"
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
	transitionOnChainTickInterval = 10 * time.Second
	resultsOnChainTickInterval    = 10 * time.Second
	maxResultsUploadRetries       = 3
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

		// check the remote state root matches the local one
		remoteStateRoot, err := s.contracts.StateRoot(pid)
		if err != nil || remoteStateRoot == nil {
			log.Errorw(err, "failed to get remote state root for: "+hex.EncodeToString(pid))
			return true // Continue to next process ID
		}
		thisStateRoot := batch.Inputs.RootHashBefore
		if remoteStateRoot.MathBigInt().Cmp(thisStateRoot) != 0 {
			log.Errorw(fmt.Errorf("state root mismatch for processId %s: local %s != remote %s",
				hex.EncodeToString(pid), thisStateRoot.String(), remoteStateRoot.String()), "could not push state transition to contract")
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
		"pid", hex.EncodeToString(processID),
		"abiProof", hex.EncodeToString(abiProof),
		"abiInputs", hex.EncodeToString(abiInputs),
		"strProof", proof.String(),
		"strInputs", inputs.String())

	// Verify the blob proof locally before sending to the contract
	if err := gethkzg.VerifyBlobProof(&blobSidecar.Blobs[0], blobSidecar.Commitments[0], blobSidecar.Proofs[0]); err != nil {
		return fmt.Errorf("local blob proof verification failed: %w", err)
	}
	log.Debugw("local blob proof verification succeeded")

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
			"pid", hex.EncodeToString(processID))
	}

	// Submit the proof to the contract
	txHash, err := s.contracts.SetProcessTransition(processID,
		abiProof,
		abiInputs,
		blobSidecar,
	)
	if err != nil {
		return fmt.Errorf("failed to submit state transition: %w", err)
	}

	// Wait for the transaction to be mined
	return s.contracts.WaitTx(*txHash, time.Minute)
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
			"abiProof", hex.EncodeToString(abiProof),
			"abiInputs", hex.EncodeToString(abiInputs),
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

			// submit the proof to the contract
			txHash, err := s.contracts.SetProcessResults(res.ProcessID, abiProof, abiInputs)
			if err != nil {
				lastErr = err
				log.Warnw("failed to upload verified results",
					"attempt", attempt+1,
					"error", err,
					"processID", res.ProcessID.String())
				continue
			}

			// wait for the transaction to be mined
			if err := s.contracts.WaitTx(*txHash, time.Second*120); err != nil {
				lastErr = err
				log.Warnw("failed to wait for verified results upload transaction",
					"attempt", attempt+1,
					"error", err,
					"processID", res.ProcessID.String())
				continue
			}

			// Success!
			log.Infow("verified results uploaded to contract",
				"pid", hex.EncodeToString(res.ProcessID),
				"txHash", txHash.String(),
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
