package sequencer

import (
	"fmt"
	"math/big"
	"time"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	groth16_bn254 "github.com/consensys/gnark/backend/groth16/bn254"
	"github.com/consensys/gnark/backend/solidity"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	"github.com/vocdoni/davinci-node/crypto/blobs"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
	imtcircuit "github.com/vocdoni/lean-imt-go/circuit"
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
	s.pids.ForEach(func(processID types.ProcessID, _ time.Time) bool {
		// Check if there is a batch ready for processing
		batch, batchID, err := s.stg.NextAggregatorBatch(processID)
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
		if s.stg.HasPendingTx(storage.StateTransitionTx, processID) {
			log.Debugw("skipping state transition processing due to pending txs",
				"processID", processID.String())
			return true // Continue to next process ID
		}

		// Lock the processor to avoid concurrent workloads
		s.workInProgressLock.Lock()
		defer s.workInProgressLock.Unlock()
		startTime := time.Now()

		// Initialize the process state (use current in-construction state)
		processState, err := s.currentProcessState(processID)
		if err != nil {
			log.Errorw(err, "failed to load process state")
			if err := s.stg.MarkAggregatorBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		// Get the root hash, this is the state before the batch
		rootHashBefore, err := processState.RootAsBigInt()
		if err != nil {
			log.Errorw(err, "failed to get root")
			if err := s.stg.MarkAggregatorBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		log.Debugw("state transition ready for processing",
			"processID", batch.ProcessID.String(),
			"ballotCount", len(batch.Ballots),
			"rootHashBefore", rootHashBefore.String(),
		)

		// Reencrypt the votes with a new k
		reencryptedVotes, kSeed, err := s.reencryptVotes(batch.ProcessID, batch.Ballots)
		if err != nil {
			log.Errorw(err, "failed to reencrypt votes")
			if err := s.stg.MarkAggregatorBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		// Get the circuit-ready census proofs for the current batch
		censusProofs := make([]*types.CensusProof, len(batch.Ballots))
		for i, b := range batch.Ballots {
			censusProofs[i] = b.CensusProof
		}
		censusRoot, circuitCensusProofs, err := s.processCensusProofs(batch.ProcessID, reencryptedVotes, censusProofs)
		if err != nil {
			log.Errorw(err, "failed to get census proofs")
			return true // Continue to next process ID
		}

		// Process the batch inner proof and votes to get the proof of the
		// state transition
		proof, blobData, rootHashAfter, err := s.processStateTransitionBatch(
			processState,
			censusRoot,
			*circuitCensusProofs,
			reencryptedVotes,
			kSeed,
			batch.Proof)
		if err != nil {
			log.Errorw(err, "failed to process state transition batch")
			if err := s.stg.MarkAggregatorBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		// Sanity check: roots must be different if voters were added
		if rootHashBefore.Cmp(rootHashAfter) == 0 && len(reencryptedVotes) > 0 {
			log.Errorw(fmt.Errorf("state root unchanged after adding %d votes", len(reencryptedVotes)),
				"failed to update state root")
			log.Debugw("state root unchanged details",
				"rootBefore", rootHashBefore.String(),
				"rootAfter", rootHashAfter.String(),
				"processID", processID.String(),
				"voteCount", len(reencryptedVotes))
			if err := s.stg.MarkAggregatorBatchFailed(batchID); err != nil {
				log.Errorw(err, "failed to mark ballot batch as failed")
			}
			return true // Continue to next process ID
		}

		// Get blob sidecar and hash
		blobSidecar := blobData.TxSidecar()

		log.Infow("state transition proof generated",
			"took", time.Since(startTime).String(),
			"pid", processID.String(),
			"rootHashBefore", rootHashBefore.String(),
			"rootHashAfter", rootHashAfter.String(),
			"blobHash", blobSidecar.BlobHashes()[0].String(),
		)

		if err := s.stg.SetPendingTx(storage.StateTransitionTx, batch.ProcessID); err != nil {
			log.Warnw("failed to mark process as having pending tx",
				"error", err,
				"processID", batch.ProcessID.String())
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
				RootHashBefore:        rootHashBefore,
				RootHashAfter:         rootHashAfter,
				VotersCount:           processState.VotersCount(),
				OverwrittenVotesCount: processState.OverwrittenVotesCount(),
				CensusRoot:            censusRoot.MathBigInt(),
				BlobCommitmentLimbs:   blobData.CommitmentLimbs,
			},
			BlobVersionHash: blobSidecar.BlobHashes()[0],
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
		s.pids.Add(processID) // This will update the timestamp
		return true           // Continue to next process ID
	})
}

func (s *Sequencer) processStateTransitionBatch(
	processState *state.State,
	censusRoot *types.BigInt,
	censusProofs statetransition.CensusProofs,
	votes []*state.Vote,
	kSeed *types.BigInt,
	innerProof groth16.Proof,
) (groth16.Proof, *blobs.BlobEvalData, *big.Int, error) {
	startTime := time.Now()
	// generate the state transition assignments from the batch and the blob data
	assignments, blobData, err := s.stateBatchToWitness(processState, votes, censusRoot, censusProofs, kSeed, innerProof)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to generate assignments: %w", err)
	}
	log.Debugw("state transition assignments ready for proof generation",
		"took", time.Since(startTime).String(),
		"processID", processState.ProcessID().String(),
		"votersCount", assignments.VotersCount,
		"overwrittenVotesCount", assignments.OverwrittenVotesCount,
		"rootHashBefore", assignments.RootHashBefore,
		"rootHashAfter", assignments.RootHashAfter,
		"censusRoot", censusRoot.String(),
	)

	// Prepare the options for the prover - use solidity verifier target
	opts := solidity.WithProverTargetSolidityVerifier(backend.GROTH16)

	// Generate the proof
	proof, err := s.prover(params.StateTransitionCurve, s.stCcs, s.stPk, assignments, opts)
	if err != nil {
		s.logStateTransitionDebugInfo(processState, votes, censusRoot, assignments, err)
		return nil, nil, nil, fmt.Errorf("failed to generate proof: %w", err)
	}
	return proof, blobData, assignments.RootHashAfter.(*big.Int), nil
}

// logStateTransitionDebugInfo logs detailed information about a failed state transition
// to help reproduce and debug constraint satisfaction errors
func (s *Sequencer) logStateTransitionDebugInfo(
	processState *state.State,
	votes []*state.Vote,
	censusRoot *types.BigInt,
	assignments *statetransition.StateTransitionCircuit,
	err error,
) {
	log.Errorw(err, "STATE TRANSITION CONSTRAINT ERROR - DEBUG INFO")
	if assignments != nil {
		log.Infow("constraint error details",
			"processID", processState.ProcessID().String(),
			"rootHashBefore", assignments.RootHashBefore,
			"rootHashAfter", assignments.RootHashAfter,
			"votersCount", assignments.VotersCount,
			"overwrittenVotesCount", assignments.OverwrittenVotesCount,
			"censusRoot", censusRoot.String(),
			"#votes", len(votes),
		)

		// Log assignment info (basic info only)
		log.Infow("assignment details available for debugging")
	} else {
		log.Warnw("assignments is nil, skipping detailed constraint error logging")
	}

	// Log vote details
	for i, v := range votes {
		log.Infow("vote details",
			"index", i,
			"voteID", v.VoteID.String(),
			"address", v.Address.String(),
			"weight", v.Weight.String(),
		)
	}
}

func (s *Sequencer) reencryptVotes(pid types.ProcessID, votes []*storage.AggregatorBallot) ([]*state.Vote, *types.BigInt, error) {
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
			Weight:            v.Weight,
			ReencryptedBallot: reencryptedBallot,
		}
	}
	log.Infow("votes reencrypted", "processID", pid.String(), "len(votes)", len(reencryptedVotes))
	return reencryptedVotes, new(types.BigInt).SetBigInt(kSeed), nil
}

func (s *Sequencer) stateBatchToWitness(
	processState *state.State,
	votes []*state.Vote,
	censusRoot *types.BigInt,
	censusProofs statetransition.CensusProofs,
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
	proofWitness, _, err := statetransition.GenerateWitness(processState, censusRoot, censusProofs, kSeed)
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
	proofWitness.BlobCommitmentLimbs = blobData.ForGnark.CommitmentLimbs
	proofWitness.BlobProofLimbs = blobData.ForGnark.ProofLimbs
	proofWitness.BlobEvaluationResultY = blobData.ForGnark.Y

	return proofWitness, blobData, nil
}

func (s *Sequencer) processCensusProofs(
	pid types.ProcessID,
	votes []*state.Vote,
	censusProofs []*types.CensusProof,
) (*types.BigInt, *statetransition.CensusProofs, error) {
	// get the process from the storage
	process, err := s.stg.Process(pid)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get process metadata: %w", err)
	}

	var root *big.Int
	merkleProofs := [params.VotesPerBatch]imtcircuit.MerkleProof{}
	cspProofs := [params.VotesPerBatch]csp.CSPProof{}
	switch {
	case process.Census.CensusOrigin.IsMerkleTree():
		// load the census from the storage
		censusRef, err := s.stg.CensusDB().LoadByRoot(process.Census.CensusRoot)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load census by root: %w", err)
		}
		// get the merkle tree and its root
		censusTree := censusRef.Tree()
		var ok bool
		if root, ok = censusTree.Root(); !ok {
			log.Warnw("census tree has no root?", "censusRoot", process.Census.CensusRoot.String(), "fetchedRoot", root.String())
		}
		// iterate over the votes to generate the merkle proofs of each voter
		for i := range params.VotesPerBatch {
			if i < len(votes) {
				addr := common.BigToAddress(votes[i].Address)
				proof, err := censusTree.GenerateProof(addr)
				if err != nil {
					return nil, nil, fmt.Errorf("error generating census proof for address %s: %w", addr.Hex(), err)
				}
				merkleProofs[i] = imtcircuit.CensusProofToMerkleProof(proof)
			} else {
				merkleProofs[i] = statetransition.DummyMerkleProof()
			}
			cspProofs[i] = statetransition.DummyCSPProof()
		}
	case process.Census.CensusOrigin.IsCSP():
		// iterate over the votes to get the CSP proofs
		root = process.Census.CensusRoot.BigInt().MathBigInt()
		for i := range params.VotesPerBatch {
			if i < len(votes) {
				proof, err := csp.CensusProofToCSPProof(process.Census.CensusOrigin.CurveID(), censusProofs[i])
				if err != nil {
					return nil, nil, fmt.Errorf("error transforming census proof for address %s: %w", common.BigToAddress(votes[i].Address).Hex(), err)
				}
				cspProofs[i] = *proof
			} else {
				cspProofs[i] = statetransition.DummyCSPProof()
			}
			merkleProofs[i] = statetransition.DummyMerkleProof()
		}
	default:
		return nil, nil, fmt.Errorf("unsupported census origin: %s", process.Census.CensusOrigin.String())
	}
	return new(types.BigInt).SetBigInt(root), &statetransition.CensusProofs{
		MerkleProofs: merkleProofs,
		CSPProofs:    cspProofs,
	}, nil
}
