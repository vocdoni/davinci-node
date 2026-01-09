package api

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

// newProcess creates a new voting process
// POST /processes
func (a *API) newProcess(w http.ResponseWriter, r *http.Request) {
	p := &types.ProcessSetup{}
	if err := json.NewDecoder(r.Body).Decode(p); err != nil {
		ErrMalformedBody.Withf("could not decode request body: %v", err).Write(w)
		return
	}

	// Unmarshal the process ID
	if !p.ProcessID.IsValid() {
		ErrMalformedProcessID.With("invalid process ID").Write(w)
		return
	}

	// Validate the census origin
	if !p.Census.CensusOrigin.Valid() {
		ErrMalformedBody.Withf("invalid census origin: %d", p.Census.CensusOrigin).Write(w)
		return
	}

	// Validate the process ID version
	if p.ProcessID.Version() != a.processIDsVersion {
		ErrInvalidContractVersion.Withf("%x", p.ProcessID.Version()).Write(w)
		return
	}

	// Extract the address from the signature
	signedMessage := fmt.Sprintf(types.NewProcessMessageToSign, p.ProcessID.String())
	address, err := ethereum.AddrFromSignature([]byte(signedMessage), new(ethereum.ECDSASignature).SetBytes(p.Signature))
	if err != nil {
		ErrInvalidSignature.Withf("could not extract address from signature: %v", err).Write(w)
		return
	}

	// Validate the ballot mode
	if err := p.BallotMode.Validate(); err != nil {
		ErrMalformedBody.Withf("invalid ballot mode: %v", err).Write(w)
		return
	}

	// Fetch the elgamal key from storage
	publicKey, _, err := a.storage.FetchOrGenerateEncryptionKeys(p.ProcessID)
	if err != nil {
		ErrGenericInternalServerError.Withf("could not fetch or generate encryption keys: %v", err).Write(w)
		return
	}

	// Prepare inputs for the state ready for the state transition circuit:
	// - the census root must be encoded according to the arbo format
	root, err := state.CalculateInitialRoot(
		p.ProcessID,
		p.Census.CensusOrigin.BigInt().MathBigInt(),
		p.BallotMode,
		publicKey)
	if err != nil {
		ErrGenericInternalServerError.Withf("could not calculate state root: %v", err).Write(w)
		return
	}

	// Create the process response
	x, y := publicKey.Point()
	pr := &types.ProcessSetupResponse{
		ProcessID:        &p.ProcessID,
		EncryptionPubKey: [2]*types.BigInt{(*types.BigInt)(x), (*types.BigInt)(y)},
		StateRoot:        root.Bytes(),
		BallotMode:       p.BallotMode,
	}

	// Write the response
	log.Infow("new process setup query",
		"address", address.String(),
		"censusOrigin", p.Census.CensusOrigin.String(),
		"censusRoot", p.Census.CensusRoot.String(),
		"censusURI", p.Census.CensusURI,
		"processId", p.ProcessID.String(),
		"pubKeyX", pr.EncryptionPubKey[0].String(),
		"pubKeyY", pr.EncryptionPubKey[1].String(),
		"stateRoot", pr.StateRoot.String(),
		"ballotMode", pr.BallotMode.String())
	httpWriteJSON(w, pr)
}

// getProcess retrieves a voting process
// GET /processes/{processId}
func (a *API) process(w http.ResponseWriter, r *http.Request) {
	// Unmarshal the process ID
	processID, err := types.HexStringToProcessID(chi.URLParam(r, ProcessURLParam))
	if err != nil {
		ErrMalformedProcessID.Withf("could not parse process ID: %v", err).Write(w)
		return
	}

	// Retrieve the process
	proc, err := a.storage.Process(processID)
	if err != nil {
		ErrProcessNotFound.Withf("could not retrieve process: %v", err).Write(w)
		return
	}

	isAcceptingVotes, _ := a.storage.ProcessIsAcceptingVotes(processID)
	// Write the response
	httpWriteJSON(w, &ProcessResponse{
		Process:          *proc,
		IsAcceptingVotes: isAcceptingVotes,
	})
}

// processList retrieves the list of voting processes
// GET /processes
func (a *API) processList(w http.ResponseWriter, r *http.Request) {
	// Retrieve the list of processes
	processes, err := a.storage.ListProcesses()
	if err != nil {
		ErrGenericInternalServerError.Withf("could not retrieve processes: %v", err).Write(w)
		return
	}
	// Write the response
	httpWriteJSON(w, &ProcessList{Processes: processes})
}

// setMetadata sets the metadata for a voting process
// POST /metadata
func (a *API) setMetadata(w http.ResponseWriter, r *http.Request) {
	// Decode the metadata from the request body
	var metadata types.Metadata
	if err := json.NewDecoder(r.Body).Decode(&metadata); err != nil {
		ErrMalformedBody.Withf("could not decode request body: %v", err).Write(w)
		return
	}

	if len(metadata.Title) == 0 {
		ErrMalformedBody.With("metadata title is required").Write(w)
		return
	}

	// Store the metadata in the storage
	hash, err := a.storage.SetMetadata(&metadata)
	if err != nil {
		ErrGenericInternalServerError.Withf("could not store process metadata: %v", err).Write(w)
		return
	}

	httpWriteJSON(w, &SetMetadataResponse{
		Hash: hash,
	})
}

// fetchMetadata retrieves the metadata for a voting process
// GET /metadata/{metadataHash}
func (a *API) fetchMetadata(w http.ResponseWriter, r *http.Request) {
	// Decode the metadata hash from the URL
	hashBytes, err := hex.DecodeString(util.TrimHex(chi.URLParam(r, MetadataHashParam)))
	if err != nil {
		ErrMalformedParam.Write(w)
		return
	}

	// Retrieve the metadata from the storage
	metadata, err := a.storage.Metadata(hashBytes)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			ErrResourceNotFound.Write(w)
			return
		}
		ErrGenericInternalServerError.Withf("could not retrieve metadata: %v", err).Write(w)
		return
	}

	httpWriteJSON(w, metadata)
}

// processParticipant retrieves information about a participant in a voting
// process
// GET /processes/{processId}/participants/{address}
func (a *API) processParticipant(w http.ResponseWriter, r *http.Request) {
	// Unmarshal the process ID from URL parameter
	processID, err := types.HexStringToProcessID(chi.URLParam(r, ProcessURLParam))
	if err != nil {
		ErrMalformedProcessID.Withf("could not parse process ID: %v", err).Write(w)
		return
	}

	// Load the process from storage
	process, err := a.storage.Process(processID)
	if err != nil {
		if err == storage.ErrNotFound {
			ErrProcessNotFound.Withf("could not retrieve process: %v", err).Write(w)
			return
		}
		ErrGenericInternalServerError.Withf("could not retrieve process: %v", err).Write(w)
		return
	}

	// Unmarshal the participant address from URL parameter
	addressStr := chi.URLParam(r, AddressURLParam)
	address := common.HexToAddress(addressStr)
	if address == (common.Address{}) {
		ErrMalformedParam.Withf("invalid participant address: %s", addressStr).Write(w)
		return
	}

	// Retrieve the participant info
	censusRoot := process.Census.CensusRoot
	census, err := a.storage.CensusDB().LoadByRoot(censusRoot)
	if err != nil {
		trimmed := censusRoot.LeftTrim()
		if len(trimmed) > 0 && !trimmed.Equal(censusRoot) {
			censusRoot = trimmed
			census, err = a.storage.CensusDB().LoadByRoot(censusRoot)
		}
	}
	if err != nil {
		ErrGenericInternalServerError.Withf("could not retrieve participant info: %v", err).Write(w)
		return
	}

	// Get the participant weight
	weight, ok := census.Tree().GetWeight(address)
	if !ok {
		ErrResourceNotFound.Withf("participant not found in census: %s", address.String()).Write(w)
		return
	}

	// Write the response
	httpWriteJSON(w, CensusParticipant{
		Key:    types.HexBytes(address.Bytes()),
		Weight: (*types.BigInt)(weight),
	})
}
