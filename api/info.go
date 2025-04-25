package api

import (
	"encoding/json"
	"net/http"

	"github.com/vocdoni/vocdoni-z-sandbox/config"
)

// info returns the information needed by the client to generate a ballot zkSNARK proof
// GET /info
func (a *API) info(w http.ResponseWriter, r *http.Request) {
	// Get the contracts configuration for the current network
	contracts := config.DefaultConfig[a.network]
	if contracts == (config.DavinciWeb3Config{}) {
		ErrGenericInternalServerError.Withf("invalid network configuration for %s", a.network).Write(w)
		return
	}

	// Build the response with the necessary circuit information
	response := &BallotProofInfo{
		CircuitURL:          config.BallotProoCircuitURL,
		CircuitHash:         config.BallotProofCircuitHash,
		ProvingKeyURL:       config.BallotProofProvingKeyURL,
		ProvingKeyHash:      config.BallotProofProvingKeyHash,
		VerificationKeyURL:  config.BallotProofVerificationKeyURL,
		VerificationKeyHash: config.BallotProofVerificationKeyHash,
		WASMhelperURL:       config.BallotProofWasmHelperURL,
		WASMhelperHash:      config.BallotProofWasmHelperHash,
		Contracts: ContractAddresses{
			ProcessRegistry:      contracts.ProcessRegistrySmartContract,
			OrganizationRegistry: contracts.OrganizationRegistrySmartContract,
			Results:              contracts.ResultsSmartContract,
		},
	}

	// Write the response
	jsonResponse, err := json.Marshal(response)
	if err != nil {
		ErrMarshalingServerJSONFailed.WithErr(err).Write(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(jsonResponse)
}
