package api

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/metadata"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

const maxMetadataBodyBytes = 1 << 20 // 1MiB

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
	// Try to get the chainID from the request query in an uint64
	chainID, _ := strconv.ParseUint(r.URL.Query().Get("chainId"), 10, 64)
	if chainID == 0 {
		// If no chainID is specified, write the response with all processes
		httpWriteJSON(w, &ProcessList{Processes: processes})
		return
	}
	// If a chain ID is specified, get the version for that chain ID
	version, ok := a.runtimes.VersionForChainID(chainID)
	if !ok {
		ErrInvalidChainID.Write(w)
		return
	}
	var filteredProcesses []types.ProcessID
	for _, p := range processes {
		if p.Version() == version {
			filteredProcesses = append(filteredProcesses, p)
		}
	}
	// Write the response with the filtered processes
	httpWriteJSON(w, &ProcessList{Processes: filteredProcesses})
}

// setMetadata sets the metadata for a voting process
// POST /metadata
func (a *API) setMetadata(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMetadataBodyBytes)
	defer func() { _ = r.Body.Close() }()

	// Decode the metadata from the request body
	var metadata types.Metadata
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&metadata); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			ErrRequestBodyTooLarge.Withf("metadata payload exceeds %d bytes", maxMetadataBodyBytes).Write(w)
			return
		}
		ErrMalformedBody.Withf("could not decode request body: %v", err).Write(w)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		ErrMalformedBody.With("request body must contain a single JSON object").Write(w)
		return
	}

	if len(metadata.Title) == 0 {
		ErrMalformedBody.With("metadata title is required").Write(w)
		return
	}

	// Store the metadata in the storage
	hash, err := a.metadata.Set(r.Context(), &metadata)
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
	key, err := hex.DecodeString(util.TrimHex(chi.URLParam(r, MetadataHashParam)))
	if err != nil {
		ErrMalformedParam.Write(w)
		return
	}

	// Retrieve the metadata from the storage
	data, err := a.metadata.Get(r.Context(), key)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			ErrResourceNotFound.Write(w)
			return
		}
		ErrGenericInternalServerError.Withf("could not retrieve metadata: %v", err).Write(w)
		return
	}

	httpWriteJSON(w, data)
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
	runtime, err := a.runtimes.RuntimeForProcess(processID)
	if err != nil {
		ErrGenericInternalServerError.Withf("could not resolve process runtime: %v", err).Write(w)
		return
	}
	censusRef, err := a.storage.LoadCensus(runtime.Contracts.ChainID, process.Census)
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
