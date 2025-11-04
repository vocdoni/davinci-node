package sequencer

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	groth16_bn254 "github.com/consensys/gnark/backend/groth16/bn254"
	"github.com/consensys/gnark/backend/solidity"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	"github.com/vocdoni/davinci-node/crypto/blobs"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
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
	// Process each registered process ID
	s.pids.ForEach(func(pid []byte, _ time.Time) bool {
		// Check if there is a batch ready for processing
		batch, batchID, err := s.stg.NextAggregatorBatch(pid)
		if err != nil {
			if err != storage.ErrNoMoreElements {
				log.Errorw(err, "failed to get next ballot batch")
			}
			return true // Continue to next process ID
		}
		// If the batch is nil, skip it
		if batch == nil || len(batch.Ballots) == 0 {
			log.Debugw("no ballots in batch", "batchID", batchID)
			if err := s.stg.MarkAggregatorBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		// If there are pending txs, skip this process ID
		if s.stg.HasPendingTx(storage.StateTransitionTx, pid) {
			log.Debugw("skipping state transition processing due to pending txs",
				"processID", fmt.Sprintf("%x", pid))
			return true // Continue to next process ID
		}

		// Decode process ID and load metadata
		processID := new(types.ProcessID).SetBytes(batch.ProcessID)

		// Lock the processor to avoid concurrent workloads
		s.workInProgressLock.Lock()
		defer s.workInProgressLock.Unlock()
		startTime := time.Now()

		// Initialize the process state
		processState, err := s.latestProcessState(processID)
		if err != nil {
			log.Errorw(err, "failed to load process state")
			if err := s.stg.MarkAggregatorBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		// Get the root hash, this is the state before the batch
		root, err := processState.RootAsBigInt()
		if err != nil {
			log.Errorw(err, "failed to get root")
			if err := s.stg.MarkAggregatorBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		log.Debugw("state transition ready for processing",
			"processID", processID.String(),
			"ballotCount", len(batch.Ballots),
			"rootHashBefore", root.String(),
		)

		// Reencrypt the votes with a new k
		reencryptedVotes, kSeed, err := s.reencryptVotes(processID, batch.Ballots)
		if err != nil {
			log.Errorw(err, "failed to reencrypt votes")
			if err := s.stg.MarkAggregatorBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		// Process the batch inner proof and votes to get the proof of the
		// state transition
		proof, blobData, err := s.processStateTransitionBatch(processState, reencryptedVotes, kSeed, batch.Proof)
		if err != nil {
			log.Errorw(err, "failed to process state transition batch")
			if err := s.stg.MarkAggregatorBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		// Get raw public inputs
		rootHashAfter, err := processState.RootAsBigInt()
		if err != nil {
			log.Errorw(err, "failed to get root hash after")
			if err := s.stg.MarkAggregatorBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		// Get blob sidecar and hash
		blobSidecar, blobHashes, err := blobData.TxSidecar()
		if err != nil {
			log.Errorw(err, "failed to get blob sidecar")
			if err := s.stg.MarkAggregatorBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		log.Infow("state transition proof generated",
			"took", time.Since(startTime).String(),
			"pid", processID.String(),
			"rootHashBefore", root.String(),
			"rootHashAfter", rootHashAfter.String(),
			"blobHash", blobHashes[0].String(),
		)

		p := proof.(*groth16_bn254.Proof)
		commitments := make([]string, len(p.Commitments))
		for i, c := range p.Commitments {
			commitments[i] = c.String()
		}

		if err := s.stg.SetPendingTx(storage.StateTransitionTx, batch.ProcessID); err != nil {
			log.Warnw("failed to mark process as having pending tx",
				"error", err,
				"processID", fmt.Sprintf("%x", processID))
		}
		if err := s.stg.MarkAggregatorBatchPending(batch); err != nil {
			log.Errorw(err, "failed to mark aggregator batch as pending, it will not be retried")
			// If the storage fails, continue to next process ID
			return true
		}

		// Store the proof in the state transition storage
		if err := s.stg.PushStateTransitionBatch(&storage.StateTransitionBatch{
			ProcessID: batch.ProcessID,
			BatchID:   batchID,
			Proof:     proof.(*groth16_bn254.Proof),
			Ballots:   batch.Ballots,
			Inputs: storage.StateTransitionBatchProofInputs{
				RootHashBefore:       processState.RootHashBefore(),
				RootHashAfter:        rootHashAfter,
				NumNewVotes:          processState.BallotCount(),
				NumOverwritten:       processState.OverwrittenCount(),
				BlobEvaluationPointZ: blobData.Z,
				BlobEvaluationPointY: blobData.Ylimbs,
				BlobCommitment:       blobData.Commitment,
				BlobProof:            blobData.OpeningProof,
			},
			BlobVersionHash: blobHashes[0],
			BlobSidecar:     blobSidecar,
		}); err != nil {
			log.Errorw(err, "failed to push state transition batch")
			if err := s.stg.MarkAggregatorBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		// Mark the batch as done
		if err := s.stg.MarkAggregatorBatchDone(batchID); err != nil {
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
	votes []*state.Vote,
	kSeed *types.BigInt,
	innerProof groth16.Proof,
) (groth16.Proof, *blobs.BlobEvalData, error) {
	startTime := time.Now()
	// generate the state transition assignments from the batch and the blob data
	assignments, blobData, err := s.stateBatchToWitness(processState, votes, kSeed, innerProof)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate assignments: %w", err)
	}
	log.Debugw("state transition assignments ready for proof generation", "took", time.Since(startTime).String())

	// Prepare the options for the prover - use solidity verifier target
	opts := solidity.WithProverTargetSolidityVerifier(backend.GROTH16)

	// Generate the proof
	proof, err := s.prover(circuits.StateTransitionCurve, s.stCcs, s.stPk, assignments, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate proof: %w", err)
	}
	return proof, blobData, nil
}

func (s *Sequencer) latestProcessState(pid *types.ProcessID) (*state.State, error) {
	// get the process from the storage
	process, err := s.stg.Process(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to get process metadata: %w", err)
	}
	isAcceptingVotes, err := s.stg.ProcessIsAcceptingVotes(pid.Marshal())
	if err != nil {
		return nil, fmt.Errorf("failed to check if process is accepting votes: %w", err)
	}
	if !isAcceptingVotes {
		return nil, fmt.Errorf("process %x is not accepting votes", pid)
	}

	st, err := state.New(s.stg.StateDB(), pid.BigInt())
	if err != nil {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}
	censusRoot, err := process.BigCensusRoot()
	if err != nil {
		return nil, fmt.Errorf("failed to get census root: %w", err)
	}

	if err := st.Initialize(
		process.Census.CensusOrigin.BigInt().MathBigInt(),
		censusRoot.MathBigInt(),
		circuits.BallotModeToCircuit(process.BallotMode),
		circuits.EncryptionKeyToCircuit(*process.EncryptionKey),
	); err != nil && !errors.Is(err, state.ErrStateAlreadyInitialized) {
		return nil, fmt.Errorf("failed to init state: %w", err)
	}

	// get the on-chain state root to ensure we are in sync
	onchainStateRoot, err := s.contracts.StateRoot(pid.Marshal())
	if err != nil {
		return nil, fmt.Errorf("failed to get on-chain state root: %w", err)
	}

	// if the on-chain state root is different from the local one, update it
	if onchainStateRoot.MathBigInt().Cmp(process.StateRoot.MathBigInt()) != 0 {
		if err := st.RootExists(onchainStateRoot.MathBigInt()); err != nil {
			return nil, fmt.Errorf("on-chain state root does not exist in local state: %w", err)
		}
		if err := s.stg.UpdateProcess(pid.Marshal(), storage.ProcessUpdateCallbackStateRoot(onchainStateRoot, nil, nil)); err != nil {
			return nil, fmt.Errorf("failed to update process state root: %w", err)
		}
		log.Warnw("local state root mismatch, updated local state root to match on-chain",
			"pid", pid.String(),
			"local", process.StateRoot.String(),
			"onchain", onchainStateRoot.String(),
		)
	}

	// initialize the process state on the given root
	processState, err := state.LoadOnRoot(s.stg.StateDB(), pid.BigInt(), onchainStateRoot.MathBigInt())
	if err != nil {
		return nil, fmt.Errorf("failed to create state: %w", err)
	}
	return processState, nil
}

func (s *Sequencer) reencryptVotes(pid *types.ProcessID, votes []*storage.AggregatorBallot) ([]*state.Vote, *types.BigInt, error) {
	// generate a initial k to reencrypt the ballots
	kSeed, err := elgamal.RandK()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate random k: %w", err)
	}
	// get encryption key from the storage
	encryptionKey, _, err := s.stg.EncryptionKeys(pid)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get encryption key: %w", err)
	}
	// iterate over the votes, reencrypting each time the zero ballot with the
	// current k, adding it to the encrypted ballot
	reencryptedVotes := make([]*state.Vote, len(votes))
	lastK := new(big.Int).Set(kSeed)
	for i, v := range votes {
		var reencryptedBallot *elgamal.Ballot
		reencryptedBallot, lastK, err = v.EncryptedBallot.Reencrypt(encryptionKey, lastK)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to reencrypt ballot: %w", err)
		}
		// sum the encrypted zero ballot with the original ballot
		reencryptedVotes[i] = &state.Vote{
			Address:           v.Address,
			VoteID:            v.VoteID,
			Ballot:            v.EncryptedBallot,
			ReencryptedBallot: reencryptedBallot,
		}
	}
	log.Infow("votes reencrypted", "processID", pid.String(), "voteCount", len(reencryptedVotes))
	return reencryptedVotes, new(types.BigInt).SetBigInt(kSeed), nil
}

func (s *Sequencer) stateBatchToWitness(
	processState *state.State,
	votes []*state.Vote,
	kSeed *types.BigInt,
	innerProof groth16.Proof,
) (*statetransition.StateTransitionCircuit, *blobs.BlobEvalData, error) {
	// start a new batch
	if err := processState.StartBatch(); err != nil {
		return nil, nil, fmt.Errorf("failed to start batch: %w", err)
	}
	// add the new ballots to the state
	for _, v := range votes {
		if err := processState.AddVote(v); err != nil {
			return nil, nil, fmt.Errorf("failed to add vote: %w", err)
		}
	}
	// end the batch
	if err := processState.EndBatch(); err != nil {
		return nil, nil, fmt.Errorf("failed to end batch: %w", err)
	}
	// generate the state transition vote witness
	proofWitness, err := statetransition.GenerateWitness(processState, kSeed)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate witness: %w", err)
	}
	proofWitness.AggregatorProof, err = stdgroth16.ValueOfProof[sw_bw6761.G1Affine, sw_bw6761.G2Affine](innerProof)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to transform recursive proof: %w", err)
	}

	// generate the KZG commitment to the blob witness
	blobData, err := processState.BuildKZGCommitment()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build KZG commitment: %w", err)
	}
	proofWitness.BlobEvaluationPointZ = blobData.ForGnark.Z
	proofWitness.BlobEvaluationResultY = blobData.ForGnark.Y

	return proofWitness, blobData, nil
}
