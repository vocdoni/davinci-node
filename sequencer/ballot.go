package sequencer

import (
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	groth16_bls12377 "github.com/consensys/gnark/backend/groth16/bls12-377"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	gnarkecdsa "github.com/consensys/gnark/std/signature/ecdsa"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types/params"
)

// startBallotProcessor starts a background goroutine that continuously processes ballots.
// It fetches unprocessed ballots from storage, verifies their validity by generating
// zero-knowledge proofs, and stores the verified ballots back in storage.
// The processor will run until the sequencer's context is canceled.
func (s *Sequencer) startBallotProcessor() error {
	const tickInterval = time.Second
	ticker := time.NewTicker(tickInterval)

	go func() {
		defer ticker.Stop()
		log.Infow("ballot processor started")

		processBallots := true

		for {
			// Check for context cancellation first
			select {
			case <-s.ctx.Done():
				log.Infow("ballot processor stopped")
				return
			default:
				// Continue processing
			}

			if processBallots {
				// Process available ballots in a loop without waiting between them
				processed := s.processAvailableBallots()

				// If no ballots were processed, wait for the ticker
				processBallots = processed
			}

			if !processBallots {
				// Wait for the ticker to check for new ballots
				select {
				case <-ticker.C:
					processBallots = true // Try processing ballots again
				case <-s.ctx.Done():
					log.Infow("ballot processor stopped")
					return
				}
			}
		}
	}()
	return nil
}

// processAvailableBallots processes all available ballots in the queue.
// Returns true if at least one ballot was processed successfully.
func (s *Sequencer) processAvailableBallots() bool {
	processed := false

	for {
		// Try to fetch the next ballot
		ballot, key, err := s.stg.NextPendingBallot()
		if err != nil {
			if !errors.Is(err, storage.ErrNoMoreElements) {
				log.Errorw(err, "failed to get next ballot")
			}
			return processed
		}

		// Skip processing if the process is not registered
		if !s.ExistsProcessID(ballot.ProcessID) {
			log.Debugw("skipping ballot, process not registered", "processID", ballot.ProcessID.String())
			continue
		}

		log.Infow("processing ballot",
			"address", hex.EncodeToString(ballot.Address.Bytes()),
			"voteID", hex.EncodeToString(ballot.VoteID),
			"processID", ballot.ProcessID.String(),
		)

		verifiedBallot, err := s.processBallot(ballot)
		if err != nil {
			log.Warnw("invalid ballot",
				"error", err.Error(),
				"ballot", ballot.String(),
			)
			if err := s.stg.RemovePendingBallot(ballot.ProcessID, key); err != nil {
				log.Warnw("failed to remove invalid ballot", "error", err.Error())
			}
			continue
		}

		// Mark the ballot as processed
		if err := s.stg.MarkBallotVerified(key, verifiedBallot); err != nil {
			log.Warnw("failed to mark ballot as processed",
				"error", err.Error(),
				"address", ballot.Address.String(),
				"processID", ballot.ProcessID.String(),
			)
			continue
		}

		processed = true
	}
}

// processBallot generates a zero-knowledge proof of a ballot's validity.
// It retrieves the process information, transforms the ballot data into circuit-compatible
// formats, and generates a cryptographic proof that the ballot is valid without revealing
// the actual vote content.
//
// Parameters:
//   - b: The ballot to process
//
// Returns a verified ballot with the generated proof, or an error if validation fails.
func (s *Sequencer) processBallot(b *storage.Ballot) (*storage.VerifiedBallot, error) {
	s.workInProgressLock.RLock()
	defer s.workInProgressLock.RUnlock()
	startTime := time.Now()
	if b == nil {
		return nil, fmt.Errorf("ballot cannot be nil")
	}

	// Validate the ballot structure
	if !b.Valid() {
		log.Warnw("invalid ballot structure", "ballot", b.String())
		return nil, fmt.Errorf("invalid ballot structure")
	}

	// Get the process metadata
	process, err := s.stg.Process(b.ProcessID)
	if err != nil {
		return nil, fmt.Errorf("failed to get process metadata: %w", err)
	}
	// Ensure the process is accepting votes
	if isAcceptingVotes, err := s.stg.ProcessIsAcceptingVotes(b.ProcessID); err != nil {
		return nil, fmt.Errorf("failed to check if process is accepting votes: %w", err)
	} else if !isAcceptingVotes {
		return nil, fmt.Errorf("process is not accepting votes")
	}
	log.Debugw("preparing ballot inputs", "processID", b.ProcessID.String())
	// Transform process data to circuit types
	inputs := new(voteverifier.VoteVerifierInputs)
	if err := inputs.FromProcessBallot(process, b); err != nil {
		return nil, fmt.Errorf("failed to prepare inputs from process and ballot: %w", err)
	}

	// Calculate inputs hash
	inputHash, err := inputs.InputsHash()
	if err != nil {
		return nil, fmt.Errorf("failed to generate inputs hash: %w", err)
	}

	// Process public key
	pubKey, err := ethcrypto.UnmarshalPubkey(b.PubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress voter public key: %w", err)
	}

	// Create the circuit assignment
	assignment := voteverifier.VerifyVoteCircuit{
		IsValid:    1,
		BallotHash: emulated.ValueOf[sw_bn254.ScalarField](inputHash),
		Address:    emulated.ValueOf[sw_bn254.ScalarField](b.Address),
		VoteID:     b.VoteID.BigInt().MathBigInt(),
		PublicKey: gnarkecdsa.PublicKey[emulated.Secp256k1Fp, emulated.Secp256k1Fr]{
			X: emulated.ValueOf[emulated.Secp256k1Fp](pubKey.X),
			Y: emulated.ValueOf[emulated.Secp256k1Fp](pubKey.Y),
		},
		Signature: gnarkecdsa.Signature[emulated.Secp256k1Fr]{
			R: emulated.ValueOf[emulated.Secp256k1Fr](b.Signature.R),
			S: emulated.ValueOf[emulated.Secp256k1Fr](b.Signature.S),
		},
		CircomProof: b.BallotProof,
	}

	log.Debugw("vote verifier inputs ready",
		"processID", b.ProcessID.String(),
		"voteID", b.VoteID.String(),
		"address", b.Address.String(),
		"inputsHash", inputHash.String(),
	)

	// Prepare the options for the prover
	opts := stdgroth16.GetNativeProverOptions(
		params.AggregatorCurve.ScalarField(),
		params.VoteVerifierCurve.ScalarField(),
	)
	log.Debugw("generating vote verification proof...", "processID", b.ProcessID.String(), "voteID", hex.EncodeToString(b.VoteID))
	proof, err := s.prover(params.VoteVerifierCurve, s.vvCcs, s.vvPk, &assignment, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to generate proof: %w", err)
	}

	if s.vvVk == nil {
		return nil, fmt.Errorf("vote verifier verifying key is not loaded")
	}
	pubAssignment := &voteverifier.VerifyVoteCircuit{
		IsValid:    1,
		BallotHash: emulated.ValueOf[sw_bn254.ScalarField](inputHash),
	}
	pubWitness, err := frontend.NewWitness(pubAssignment, params.VoteVerifierCurve.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return nil, fmt.Errorf("failed to create public witness: %w", err)
	}
	verifyOpts := stdgroth16.GetNativeVerifierOptions(
		params.AggregatorCurve.ScalarField(),
		params.VoteVerifierCurve.ScalarField(),
	)
	if err := groth16.Verify(proof, s.vvVk, pubWitness, verifyOpts); err != nil {
		return nil, fmt.Errorf("failed to verify generated proof: %w", err)
	}

	log.Infow("vote verification proof generated",
		"processID", b.ProcessID.String(),
		"voteID", hex.EncodeToString(b.VoteID),
		"address", hex.EncodeToString(b.Address.Bytes()),
		"took", time.Since(startTime).String(),
	)

	proofBLS, ok := proof.(*groth16_bls12377.Proof)
	if !ok {
		return nil, fmt.Errorf("unexpected vote verifier proof type: %T", proof)
	}

	// Create and return the verified ballot
	return &storage.VerifiedBallot{
		VoteID:          b.VoteID,
		ProcessID:       b.ProcessID,
		VoterWeight:     b.VoterWeight,
		EncryptedBallot: b.EncryptedBallot,
		Address:         b.Address,
		Proof:           proofBLS,
		InputsHash:      inputHash,
		CensusProof:     b.CensusProof,
	}, nil
}
