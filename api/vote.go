package api

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/ballotproof"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/signatures/ethereum"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// newVote creates a new voting process
// POST /vote
func (a *API) newVote(w http.ResponseWriter, r *http.Request) {
	// decode the vote
	vote := &Vote{}
	if err := json.NewDecoder(r.Body).Decode(vote); err != nil {
		ErrMalformedBody.Withf("could not decode request body: %v", err).Write(w)
		return
	}
	// sanity checks
	if vote.Ballot == nil || vote.Nullifier == nil || vote.Commitment == nil ||
		vote.CensusProof.Key == nil || vote.CensusProof.Weight == nil ||
		vote.BallotInputsHash == nil || vote.Address == nil || vote.Signature == nil {
		ErrMalformedBody.Withf("missing required fields").Write(w)
		return
	}
	// get the process from the storage
	pid := new(types.ProcessID)
	if err := pid.Unmarshal(vote.ProcessID); err != nil {
		ErrMalformedBody.Withf("could not decode process id: %v", err).Write(w)
		return
	}
	process, err := a.storage.Process(pid)
	if err != nil {
		ErrResourceNotFound.Withf("could not get process: %v", err).Write(w)
		return
	}
	// check that the census root is the same as the one in the process
	if !bytes.Equal(process.Census.CensusRoot, vote.CensusProof.Root) {
		ErrInvalidCensusProof.Withf("census root mismatch").Write(w)
		return
	}
	// verify the census proof
	if !a.storage.CensusDB().VerifyProof(&vote.CensusProof) {
		ErrInvalidCensusProof.Withf("census proof verification failed").Write(w)
		return
	}
	// load the verification key for the ballot proof circuit, used by the user
	// to generate a proof of a valid ballot
	if err := ballotproof.Artifacts.LoadAll(); err != nil {
		ErrGenericInternalServerError.Withf("could not load artifacts: %v", err).Write(w)
		return
	}
	// convert the circom proof to gnark proof and verify it
	proof, err := circuits.VerifyAndConvertToRecursion(
		ballotproof.Artifacts.VerifyingKey(),
		vote.BallotProof,
		[]string{vote.BallotInputsHash.String()},
	)
	if err != nil {
		ErrInvalidBallotProof.Withf("could not verify and convert proof: %v", err).Write(w)
		return
	}
	// verify the signature of the vote
	signature := new(ethereum.ECDSASignature).SetBytes(vote.Signature)
	if signature == nil {
		ErrMalformedBody.Withf("could not decode signature: %v", err).Write(w)
		return
	}
	signatureOk, pubkey := signature.VerifyBLS12377(vote.BallotInputsHash.MathBigInt(), common.BytesToAddress(vote.Address))
	if !signatureOk {
		ErrInvalidSignature.Write(w)
		return
	}

	// push the ballot to the sequencer storage queue to be verified, aggregated
	// and published
	if err := a.storage.PushBallot(&storage.Ballot{
		ProcessID:        vote.ProcessID,
		VoterWeight:      vote.CensusProof.Weight.MathBigInt(),
		EncryptedBallot:  *vote.Ballot,
		Nullifier:        vote.Nullifier.MathBigInt(),
		Commitment:       vote.Commitment.MathBigInt(),
		Address:          vote.CensusProof.Key.BigInt().MathBigInt(),
		BallotInputsHash: vote.BallotInputsHash.MathBigInt(),
		BallotProof:      proof.Proof,
		Signature:        signature,
		CensusProof:      vote.CensusProof,
		PubKey:           pubkey,
	}); err != nil {
		ErrGenericInternalServerError.Withf("could not push ballot: %v", err).Write(w)
		return
	}
	httpWriteOK(w)
}
