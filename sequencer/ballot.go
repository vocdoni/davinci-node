package sequencer

import (
	"errors"
	"fmt"
	"time"

	groth16_bls12377 "github.com/consensys/gnark/backend/groth16/bls12-377"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	gnarkecdsa "github.com/consensys/gnark/std/signature/ecdsa"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
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
		if !s.contractsResolver.SupportsProcess(ballot.ProcessID) {
			log.Debugw("removing ballot, process not supported", "processID", ballot.ProcessID.String())
			if err := s.stg.RemovePendingBallotsByProcess(ballot.ProcessID); err != nil {
				log.Warnw("failed to remove ballots", "error", err.Error())
			}
			continue
		}

		// Skip processing if the process is not registered
		if !s.ExistsProcessID(ballot.ProcessID) {
			log.Debugw("skipping ballot, process not registered", "processID", ballot.ProcessID.String())
			continue
		}

		log.Infow("processing ballot",
			"address", types.HexBytes(ballot.Address.Bytes()),
			"voteID", ballot.VoteID.String(),
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
				"address", types.HexBytes(ballot.Address.Bytes()),
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

	// Ensure the process is accepting votes
	if isAcceptingVotes, err := s.stg.ProcessIsAcceptingVotes(b.ProcessID); err != nil {
		return nil, fmt.Errorf("failed to check if process is accepting votes: %w", err)
	} else if !isAcceptingVotes {
		return nil, fmt.Errorf("process is not accepting votes")
	}
	// Process public key
	pubKey, err := ethcrypto.UnmarshalPubkey(b.PubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress voter public key: %w", err)
	}

	// Create the circuit assignment
	assignment := voteverifier.VerifyVoteCircuit{
		IsValid:    1,
		BallotHash: emulated.ValueOf[sw_bn254.ScalarField](b.BallotInputsHash),
		Address:    emulated.ValueOf[sw_bn254.ScalarField](b.Address),
		VoteID:     b.VoteID.BigInt(),
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
		"address", types.HexBytes(b.Address.Bytes()),
		"inputsHash", b.BallotInputsHash.String(),
	)

	log.Debugw("generating vote verification proof...", "processID", b.ProcessID.String(), "voteID", b.VoteID.String())
	proof, err := s.voteVerifier.ProveAndVerify(&assignment)
	if err != nil {
		return nil, fmt.Errorf("failed to generate proof: %w", err)
	}

	log.InfoTime("vote verification proof generated", startTime,
		"processID", b.ProcessID.String(),
		"voteID", b.VoteID.String(),
		"address", types.HexBytes(b.Address.Bytes()),
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
		InputsHash:      b.BallotInputsHash,
		CensusProof:     b.CensusProof,
	}, nil
}
