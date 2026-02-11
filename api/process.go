package api

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

// processEncryptionKeys creates a new encryption key
// POST /processes/keys
func (a *API) processEncryptionKeys(w http.ResponseWriter, r *http.Request) {
	// Fetch or create the elgamal key from storage
	publicKey, _, err := a.storage.GenerateProcessEncryptionKeys()
	if err != nil {
		ErrGenericInternalServerError.Withf("could not fetch or generate encryption keys: %v", err).Write(w)
		return
	}

	// Create the process response
	x, y := publicKey.Point()
	pr := &types.ProcessEncryptionKeysResponse{
		EncryptionPubKey: [2]*types.BigInt{
			new(types.BigInt).SetBigInt(x),
			new(types.BigInt).SetBigInt(y),
		},
	}

	// Write the response
	log.Infow("new process encryption keys query",
		"pubKeyX", pr.EncryptionPubKey[0].String(),
		"pubKeyY", pr.EncryptionPubKey[1].String())
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
	censusRef, err := a.storage.LoadCensus(process.Census)
	if err != nil {
		ErrGenericInternalServerError.Withf("could not retrieve participant info: %v", err).Write(w)
		return
	}
	if censusRef == nil {
		ErrMalformedParam.With("census not compatible with local processing").Write(w)
		return
	}

	// Get the participant weight
	weight, ok := censusRef.Tree().GetWeight(address)
	if !ok {
		ErrResourceNotFound.Withf("participant not found in census: %s", address.String()).Write(w)
		return
	} else if weight.Cmp(big.NewInt(0)) == 0 {
		ErrResourceNotFound.Withf("participant has zero weight in census: %s", address.String()).Write(w)
		return
	}

	// Write the response
	httpWriteJSON(w, CensusParticipant{
		Key:    types.HexBytes(address.Bytes()),
		Weight: (*types.BigInt)(weight),
	})
}
