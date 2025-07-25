package api

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/vocdoni/arbo"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

// voteStatus returns the status of a vote for a given processID and voteID
// GET /votes/{processId}/voteId/{voteId}
func (a *API) voteStatus(w http.ResponseWriter, r *http.Request) {
	// Get the processID and voteID from the URL
	processIDHex := chi.URLParam(r, ProcessURLParam)
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

	// Get the vote ID status
	status, err := a.storage.VoteIDStatus(processID, voteID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			ErrResourceNotFound.WithErr(err).Write(w)
			return
		}
		ErrGenericInternalServerError.WithErr(err).Write(w)
		return
	}

	response := VoteStatusResponse{
		Status: storage.VoteIDStatusName(status),
	}
	httpWriteJSON(w, response)
}

// voteByAddress retrieves an encrypted ballot by its address for a given
// processID
// GET /votes/{processId}/address/{address}
func (a *API) voteByAddress(w http.ResponseWriter, r *http.Request) {
	// Get the processID
	processIDBytes, err := types.HexStringToHexBytes(chi.URLParam(r, ProcessURLParam))
	if err != nil {
		ErrMalformedProcessID.Withf("could not decode process ID: %v", err).Write(w)
		return
	}
	processID := new(types.ProcessID).SetBytes(processIDBytes)

	// Get the address
	address, err := types.HexStringToHexBytes(chi.URLParam(r, VoteByAddressAddressParam))
	if err != nil {
		ErrMalformedAddress.Write(w)
		return
	}

	// Open the state for the process
	s, err := state.New(a.storage.StateDB(), processID.BigInt())
	if err != nil {
		ErrProcessNotFound.Withf("could not open state: %v", err).Write(w)
		return
	}
	defer func() {
		if err := s.Close(); err != nil {
			log.Warnw("could not close state", "processID", processID.String(), "error", err.Error())
		}
	}()

	// Get the ballot by address
	ballot, err := s.EncryptedBallot(address.BigInt().MathBigInt())
	if err != nil {
		if errors.Is(err, arbo.ErrKeyNotFound) {
			ErrResourceNotFound.Write(w)
			return
		}
		ErrGenericInternalServerError.Withf("could not get commitment: %v", err).Write(w)
		return
	}

	httpWriteJSON(w, ballot)
}

// newVote creates a new vote and pushes it to the sequencer queue
// POST /votes
func (a *API) newVote(w http.ResponseWriter, r *http.Request) {
	// decode the vote
	vote := &Vote{}
	if err := json.NewDecoder(r.Body).Decode(vote); err != nil {
		ErrMalformedBody.Withf("could not decode request body: %v", err).Write(w)
		return
	}
	// sanity checks
	if vote.Ballot == nil || vote.BallotInputsHash == nil ||
		vote.Address == nil || vote.Signature == nil {
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
	// check that the process is ready to accept votes, it does not mean that
	// the vote will be accepted, but it is a precondition to accept the vote,
	// for example, if the process is not in this sequencer, the vote will be
	// rejected
	if ok, err := a.storage.ProcessIsAcceptingVotes(pid.Marshal()); !ok {
		ErrProcessNotAcceptingVotes.WithErr(err).Write(w)
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
	// calculate the ballot inputs hash
	ballotInputsHash, err := ballotproof.BallotInputsHash(
		vote.ProcessID,
		process.BallotMode,
		new(bjj.BJJ).SetPoint(process.EncryptionKey.X.MathBigInt(), process.EncryptionKey.Y.MathBigInt()),
		vote.Address,
		vote.VoteID.BigInt(),
		vote.Ballot.FromTEtoRTE(),
		vote.CensusProof.Weight,
	)
	if err != nil {
		ErrGenericInternalServerError.Withf("could not calculate ballot inputs hash: %v", err).Write(w)
		return
	}
	if vote.BallotInputsHash.String() != ballotInputsHash.String() {
		ErrInvalidBallotInputsHash.Withf("ballot inputs hash mismatch").Write(w)
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
		ballotproof.Artifacts.RawVerifyingKey(),
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
	signatureOk, pubkey := signature.Verify(vote.VoteID, common.BytesToAddress(vote.Address))
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
		Address:          vote.Address.BigInt().MathBigInt(),
		BallotInputsHash: vote.BallotInputsHash.MathBigInt(),
		BallotProof:      proof.Proof,
		Signature:        signature,
		CensusProof:      &vote.CensusProof,
		PubKey:           pubkey,
		VoteID:           vote.VoteID,
	}

	// push the ballot to the sequencer storage queue to be verified, aggregated
	// and published
	if err := a.storage.PushBallot(ballot); err != nil {
		switch {
		case errors.Is(err, storage.ErroBallotAlreadyExists):
			ErrBallotAlreadySubmitted.Write(w)
			return
		case errors.Is(err, storage.ErrNullifierProcessing):
			ErrBallotAlreadyProcessing.Write(w)
			return
		default:
			ErrGenericInternalServerError.Withf("could not push ballot: %v", err).Write(w)
			return
		}
	}

	httpWriteOK(w)
}
