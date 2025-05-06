package sequencer

import (
	"fmt"
	"math/big"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	groth16_bls12377 "github.com/consensys/gnark/backend/groth16/bls12-377"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	gnarkecdsa "github.com/consensys/gnark/std/signature/ecdsa"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/vocdoni/arbo"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/voteverifier"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/storage/census"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
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

		for {
			select {
			case <-s.ctx.Done():
				log.Infow("ballot processor stopped")
				return
			default:
				// Continue processing
			}

			// Try to fetch the next ballot
			ballot, key, err := s.stg.NextBallot()
			if err != nil {
				if err != storage.ErrNoMoreElements {
					log.Errorw(err, "failed to get next ballot")
				} else {
					// If no ballot is available, wait for the next tick or context cancellation
					select {
					case <-ticker.C:
					case <-s.ctx.Done():
						log.Infow("ballot processor stopped")
						return
					}
				}
				continue
			}

			// Skip processing if the process is not registered
			if !s.ExistsProcessID(ballot.ProcessID) {
				continue
			}

			// Process the ballot
			log.Debugw("processing ballot", "address", ballot.Address.String())
			startTime := time.Now()

			verifiedBallot, err := s.processBallot(ballot)
			if err != nil {
				log.Warnw("invalid ballot",
					"address", ballot.Address.String(),
					"error", err.Error(),
					"processID", fmt.Sprintf("%x", ballot.ProcessID),
				)
				continue
			}

			// Mark the ballot as processed
			if err := s.stg.MarkBallotDone(key, verifiedBallot); err != nil {
				log.Warnw("failed to mark ballot as processed",
					"error", err.Error(),
					"address", ballot.Address.String(),
					"processID", fmt.Sprintf("%x", ballot.ProcessID),
				)
				continue
			}

			log.Debugw("ballot processed successfully",
				"address", ballot.Address.String(),
				"processID", fmt.Sprintf("%x", ballot.ProcessID),
				"duration", time.Since(startTime).String(),
			)
		}
	}()
	return nil
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

	if b == nil {
		return nil, fmt.Errorf("ballot cannot be nil")
	}

	// Validate the ballot structure
	if !b.Valid() {
		return nil, fmt.Errorf("invalid ballot structure")
	}

	// Get the process metadata
	pid := new(types.ProcessID).SetBytes(b.ProcessID)
	process, err := s.stg.Process(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to get process metadata: %w", err)
	}

	// Transform process data to circuit types
	processID := crypto.BigToFF(circuits.BallotProofCurve.ScalarField(), b.ProcessID.BigInt().MathBigInt())
	root := arbo.BytesToBigInt(process.Census.CensusRoot)
	ballotMode := circuits.BallotModeToCircuit(process.BallotMode)
	encryptionKey := circuits.EncryptionKeyToCircuit(*process.EncryptionKey)

	// Calculate inputs hash
	hashInputs := make([]*big.Int, 0, 8+len(b.EncryptedBallot.BigInts()))
	hashInputs = append(hashInputs, processID)
	hashInputs = append(hashInputs, root)
	hashInputs = append(hashInputs, ballotMode.Serialize()...)
	hashInputs = append(hashInputs, encryptionKey.Serialize()...)
	hashInputs = append(hashInputs, b.Address)
	hashInputs = append(hashInputs, b.Commitment)
	hashInputs = append(hashInputs, b.Nullifier)
	hashInputs = append(hashInputs, b.EncryptedBallot.BigInts()...)

	inputHash, err := mimc7.Hash(hashInputs, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to hash inputs: %w", err)
	}

	// Process census proof
	siblings, err := census.BigIntSiblings(b.CensusProof.Siblings)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack census proof siblings: %w", err)
	}

	// Convert siblings to emulated elements
	emulatedSiblings := [types.CensusTreeMaxLevels]emulated.Element[sw_bn254.ScalarField]{}
	for i, s := range circuits.BigIntArrayToN(siblings, types.CensusTreeMaxLevels) {
		emulatedSiblings[i] = emulated.ValueOf[sw_bn254.ScalarField](s)
	}

	// Process public key
	pubKey, err := ethcrypto.UnmarshalPubkey(b.PubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress voter public key: %w", err)
	}

	// Create the circuit assignment
	assignment := voteverifier.VerifyVoteCircuit{
		IsValid:    1,
		InputsHash: emulated.ValueOf[sw_bn254.ScalarField](inputHash),
		Vote: circuits.EmulatedVote[sw_bn254.ScalarField]{
			Address:    emulated.ValueOf[sw_bn254.ScalarField](b.Address),
			Commitment: emulated.ValueOf[sw_bn254.ScalarField](b.Commitment),
			Nullifier:  emulated.ValueOf[sw_bn254.ScalarField](b.Nullifier),
			Ballot:     *b.EncryptedBallot.ToGnarkEmulatedBN254(),
		},
		UserWeight: emulated.ValueOf[sw_bn254.ScalarField](b.VoterWeight),
		Process: circuits.Process[emulated.Element[sw_bn254.ScalarField]]{
			ID:            emulated.ValueOf[sw_bn254.ScalarField](processID),
			CensusRoot:    emulated.ValueOf[sw_bn254.ScalarField](root),
			EncryptionKey: encryptionKey.BigIntsToEmulatedElementBN254(),
			BallotMode:    ballotMode.BigIntsToEmulatedElementBN254(),
		},
		CensusSiblings: emulatedSiblings,
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

	// Generate the proof
	witness, err := frontend.NewWitness(assignment, circuits.VoteVerifierCurve.ScalarField())
	if err != nil {
		return nil, fmt.Errorf("failed to create witness: %w", err)
	}

	proof, err := groth16.Prove(s.voteCcs, s.voteProvingKey, witness, stdgroth16.GetNativeProverOptions(
		circuits.AggregatorCurve.ScalarField(),
		circuits.VoteVerifierCurve.ScalarField()))
	if err != nil {
		return nil, fmt.Errorf("failed to generate proof: %w", err)
	}

	// Create and return the verified ballot
	return &storage.VerifiedBallot{
		ProcessID:       b.ProcessID,
		VoterWeight:     b.VoterWeight,
		Nullifier:       b.Nullifier,
		Commitment:      b.Commitment,
		EncryptedBallot: b.EncryptedBallot,
		Address:         b.Address,
		Proof:           proof.(*groth16_bls12377.Proof),
		InputsHash:      inputHash,
	}, nil
}
