package api

import (
	"encoding/json"
	"expvar"
	"net/http"
	"strconv"

	npbindings "github.com/vocdoni/davinci-contracts/golang-types"
	"github.com/vocdoni/davinci-node/config"
)

// info returns the information needed by the client to generate a ballot zkSNARK proof
// GET /info
func (a *API) info(w http.ResponseWriter, r *http.Request) {
	_, ok := npbindings.AvailableNetworksByName[a.network]
	if !ok {
		ErrGenericInternalServerError.Withf("invalid network configuration for %s", a.network).Write(w)
		return
	}
	contracts := npbindings.GetAllContractAddresses(a.network)
	// Build the response with the necessary circuit information
	response := &SequencerInfo{
		CircuitURL:           config.BallotProofCircuitURL,
		CircuitHash:          config.BallotProofCircuitHash,
		ProvingKeyURL:        config.BallotProofProvingKeyURL,
		ProvingKeyHash:       config.BallotProofProvingKeyHash,
		VerificationKeyURL:   config.BallotProofVerificationKeyURL,
		VerificationKeyHash:  config.BallotProofVerificationKeyHash,
		WASMhelperURL:        config.BallotProofWasmHelperURL,
		WASMhelperHash:       config.BallotProofWasmHelperHash,
		WASMhelperExecJsURL:  config.BallotProofWasmExecJsURL,
		WASMhelperExecJsHash: config.BallotProofWasmExecJsHash,
		Contracts: ContractAddresses{
			ProcessRegistry:           contracts[npbindings.ProcessRegistryContract],
			OrganizationRegistry:      contracts[npbindings.OrganizationRegistryContract],
			StateTransitionZKVerifier: contracts[npbindings.StateTransitionVerifierGroth16Contract],
			ResultsZKVerifier:         contracts[npbindings.ResultsVerifierGroth16Contract],
		},
		Network: map[string]uint32{
			a.network: npbindings.AvailableNetworksByName[a.network],
		},
	}
	// if the sequencer has a signer, include the sequencer address
	if a.sequencerSigner != nil {
		response.SequencerAddress = a.sequencerSigner.Address().Bytes()
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

// hostLoad reports expvar system metrics in a typed JSON object.
// GET /hostLoad
func (a *API) hostLoad(w http.ResponseWriter, _ *http.Request) {
	var resp HostLoadResponse

	expvar.Do(func(kv expvar.KeyValue) {
		switch kv.Key {
		case "cmdline":
			// explicitly skip
		case "memstats":
			// kv.Value.String() is already JSON
			_ = json.Unmarshal([]byte(kv.Value.String()), &resp.MemStats)

		case "host_load1":
			resp.HostLoad1, _ = strconv.ParseFloat(kv.Value.String(), 64)

		case "host_mem_used_percent":
			resp.HostMemUsedPercent, _ = strconv.ParseFloat(kv.Value.String(), 64)

		case "host_disk_used_percent":
			_ = json.Unmarshal([]byte(kv.Value.String()), &resp.HostDiskUsedPercent)
		}
	})

	jsonResponse, err := json.Marshal(resp)
	if err != nil {
		ErrMarshalingServerJSONFailed.WithErr(err).Write(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(jsonResponse)
}
