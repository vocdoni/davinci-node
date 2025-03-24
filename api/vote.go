package api

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/ballotproof"
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
		[]string{vote.BallotInputsHash.BigInt().String()},
	)
	if err != nil {
		ErrInvalidBallotProof.Withf("could not verify and convert proof: %v", err).Write(w)
		return
	}
	// verify the signature of the vote
	if !vote.Signature.Verify(vote.BallotInputsHash.BigInt().MathBigInt(), vote.PublicKey) {
		ErrInvalidSignature.Withf("invalid vote signature").Write(w)
		return
	}
	// push the ballot to the sequencer storage queue to be verified, aggregated
	// and published
	if err := a.storage.PushBallot(&storage.Ballot{
		ProcessID:        vote.ProcessID,
		VoterWeight:      vote.CensusProof.Weight.Bytes(),
		EncryptedBallot:  *vote.Ballot,
		Nullifier:        vote.Nullifier,
		Commitment:       vote.Commitment,
		Address:          vote.CensusProof.Key,
		BallotInputsHash: vote.BallotInputsHash,
		BallotProof:      proof.Proof,
		Signature:        vote.Signature,
		CensusProof:      vote.CensusProof,
		PubKey:           vote.PublicKey,
	}); err != nil {
		ErrGenericInternalServerError.Withf("could not push ballot: %v", err).Write(w)
		return
	}
	httpWriteOK(w)
}
