package api

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/ballotproof"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/signatures/ethereum"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"github.com/vocdoni/vocdoni-z-sandbox/util"
)

// voteStatus returns the status of a vote for a given processID and voteID
// GET /votes/status/{processId}/{voteId}
func (a *API) voteStatus(w http.ResponseWriter, r *http.Request) {
	// Get the processID and voteID from the URL
	processIDHex := chi.URLParam(r, VoteStatusProcessIDParam)
	voteIDHex := chi.URLParam(r, VoteStatusVoteIDParam)

	processID, err := hex.DecodeString(util.TrimHex(processIDHex))
	if err != nil {
		ErrMalformedProcessID.Withf("could not decode process ID: %v", err).Write(w)
		return
	}

	voteID, err := hex.DecodeString(util.TrimHex(voteIDHex))
	if err != nil {
		ErrMalformedBody.Withf("could not decode vote ID: %v", err).Write(w)
		return
	}

	// Get the ballot status
	status, err := a.storage.BallotStatus(processID, voteID)
	if err != nil {
		log.Debugw("ballot status not found", "processID", processIDHex, "voteID", voteIDHex, "error", err)
		ErrResourceNotFound.WithErr(err).Write(w)
		return
	}

	// Return the status response
	response := VoteStatusResponse{
		Status: storage.BallotStatusName(status),
	}
	httpWriteJSON(w, response)
}

// newVote creates a new vote and pushes it to the storage queue
// POST /votes
func (a *API) newVote(w http.ResponseWriter, r *http.Request) {
	// decode the vote
	vote := &Vote{}
	if err := json.NewDecoder(r.Body).Decode(vote); err != nil {
		ErrMalformedBody.Withf("could not decode request body: %v", err).Write(w)
		return
	}
	// sanity checks
	if vote.Ballot == nil || vote.Nullifier == nil || vote.Commitment == nil ||
		vote.BallotInputsHash == nil || vote.Address == nil || vote.Signature == nil {
		ErrMalformedBody.Withf("missing required fields").Write(w)
		return
	}
	if !vote.CensusProof.Valid() {
		ErrMalformedBody.Withf("invalid census proof").Write(w)
		return
	}
	if !vote.Ballot.Valid() {
		ErrMalformedBody.Withf("invalid ballot").Write(w)
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
	// Create the ballot object
	ballot := &storage.Ballot{
		ProcessID:   vote.ProcessID,
		VoterWeight: vote.CensusProof.Weight.MathBigInt(),
		// convert the ballot from TE (circom) to RTE (gnark)
		EncryptedBallot:  vote.Ballot.FromTEtoRTE(),
		Nullifier:        vote.Nullifier.MathBigInt(),
		Commitment:       vote.Commitment.MathBigInt(),
		Address:          vote.Address.BigInt().MathBigInt(),
		BallotInputsHash: vote.BallotInputsHash.MathBigInt(),
		BallotProof:      proof.Proof,
		Signature:        signature,
		CensusProof:      &vote.CensusProof,
		PubKey:           pubkey,
	}

	// push the ballot to the sequencer storage queue to be verified, aggregated
	// and published
	if err := a.storage.PushBallot(ballot); err != nil {
		ErrGenericInternalServerError.Withf("could not push ballot: %v", err).Write(w)
		return
	}

	// Get the vote ID and return it to the client
	voteID := ballot.VoteID()

	// Return the voteID to the client using the VoteResponse struct
	response := VoteResponse{
		VoteID: voteID,
	}
	httpWriteJSON(w, response)
}
