package api

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	"github.com/vocdoni/davinci-node/crypto/csp"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
	"github.com/vocdoni/davinci-node/util/circomgnark"
)

// voteStatus returns the status of a vote for a given processID and voteID
// GET /votes/{processId}/voteId/{voteId}
func (a *API) voteStatus(w http.ResponseWriter, r *http.Request) {
	// Get the processID and voteID from the URL
	processID, err := types.HexStringToProcessID(chi.URLParam(r, ProcessURLParam))
	if err != nil {
		ErrMalformedProcessID.Withf("could not decode process ID: %v", err).Write(w)
		return
	}

	voteID, err := hex.DecodeString(util.TrimHex(chi.URLParam(r, VoteIDURLParam)))
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
	processID, err := types.HexStringToProcessID(chi.URLParam(r, ProcessURLParam))
	if err != nil {
		ErrMalformedProcessID.Withf("could not decode process ID: %v", err).Write(w)
		return
	}

	// Get the address
	address, err := types.HexStringToHexBytes(chi.URLParam(r, AddressURLParam))
	if err != nil {
		ErrMalformedAddress.Write(w)
		return
	}

	// Open the state for the process
	s, err := state.New(a.storage.StateDB(), processID)
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
		if errors.Is(err, state.ErrKeyNotFound) {
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
	if !vote.Ballot.Valid() {
		ErrMalformedBody.Withf("invalid ballot").Write(w)
		return
	}
	// get the process from the storage
	process, err := a.storage.Process(*vote.ProcessID)
	if err != nil {
		ErrResourceNotFound.Withf("could not get process: %v", err).Write(w)
		return
	}
	// overwrite census origin with the process one to avoid inconsistencies
	// and check the census proof with it
	vote.CensusProof.CensusOrigin = process.Census.CensusOrigin
	// validate the census origin
	if !vote.CensusProof.CensusOrigin.Valid() {
		ErrMalformedBody.Withf("invalid process census origin").Write(w)
		return
	}
	// validate the census proof
	if !vote.CensusProof.Valid() {
		ErrMalformedBody.Withf("invalid census proof").Write(w)
		return
	}
	// check that the process is ready to accept votes, it does not mean that
	// the vote will be accepted, but it is a precondition to accept the vote,
	// for example, if the process is not in this sequencer, the vote will be
	// rejected
	if ok, err := a.storage.ProcessIsAcceptingVotes(*vote.ProcessID); !ok {
		if err != nil {
			ErrProcessNotAcceptingVotes.WithErr(err).Write(w)
			return
		}
		ErrProcessNotAcceptingVotes.Write(w)
		return
	}
	// check if the address has already voted, to determine if the vote is an
	// overwrite or a new vote, if so check if the process has reached max
	// voters
	isOverwrite, err := state.HasAddressVoted(a.storage.StateDB(), *process.ID, process.StateRoot, vote.Address.BigInt())
	if err != nil {
		ErrGenericInternalServerError.Withf("error checking if address has voted: %v", err).Write(w)
		return
	}
	if !isOverwrite {
		if maxVotersReached, err := a.storage.ProcessMaxVotersReached(*vote.ProcessID); err != nil {
			ErrGenericInternalServerError.Withf("could not check max voters: %v", err).Write(w)
			return
		} else if maxVotersReached {
			ErrProcessMaxVotersReached.Write(w)
			return
		}
	}
	// verify the census proof accordingly to the census origin and get the
	// voter weight
	var voterWeight *types.BigInt
	switch {
	case process.Census.CensusOrigin.IsMerkleTree():
		// load the census from the census DB
		censusRoot := process.Census.CensusRoot
		censusRef, err := a.storage.CensusDB().LoadByRoot(censusRoot)
		if err != nil {
			trimmed := censusRoot.LeftTrim()
			if len(trimmed) > 0 && !trimmed.Equal(censusRoot) {
				censusRoot = trimmed
				censusRef, err = a.storage.CensusDB().LoadByRoot(censusRoot)
			}
		}
		if err != nil {
			ErrGenericInternalServerError.Withf("could not load census: %v", err).Write(w)
			return
		}
		// verify the census proof
		weight, exists := censusRef.Tree().GetWeight(common.BytesToAddress(vote.Address))
		if !exists {
			ErrInvalidCensusProof.Withf("address not in census").Write(w)
			return
		}
		// overwrite the voter weight with the one from the census
		voterWeight = new(types.BigInt).SetBigInt(weight)
	case process.Census.CensusOrigin.IsCSP():
		if err := csp.VerifyCensusProof(&vote.CensusProof); err != nil {
			ErrInvalidCensusProof.Withf("census proof verification failed").WithErr(err).Write(w)
			return
		}
		voterWeight = vote.CensusProof.Weight
	default:
		ErrInvalidCensusProof.Withf("unsupported census origin").Write(w)
		return
	}
	// calculate the ballot inputs hash
	ballotInputsHash, err := ballotproof.BallotInputsHash(
		*vote.ProcessID,
		process.BallotMode,
		new(bjj.BJJ).SetPoint(process.EncryptionKey.X.MathBigInt(), process.EncryptionKey.Y.MathBigInt()),
		vote.Address,
		vote.VoteID.BigInt(),
		vote.Ballot.FromTEtoRTE(),
		voterWeight,
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
	proof, err := circomgnark.VerifyAndConvertToRecursion(
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
		ProcessID:   *vote.ProcessID,
		VoterWeight: voterWeight.MathBigInt(),
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
	// and published. The address locking is handled atomically inside PushPendingBallot
	if err := a.storage.PushPendingBallot(ballot); err != nil {
		switch {
		case errors.Is(err, storage.ErroBallotAlreadyExists):
			ErrBallotAlreadySubmitted.Write(w)
			return
		case errors.Is(err, storage.ErrNullifierProcessing):
			ErrBallotAlreadyProcessing.Write(w)
			return
		case errors.Is(err, storage.ErrAddressProcessing):
			ErrAddressAlreadyProcessing.Write(w)
			return
		default:
			ErrGenericInternalServerError.Withf("could not push ballot: %v", err).Write(w)
			return
		}
	}

	httpWriteOK(w)
}
