package api

import (
	"encoding/json"
	"expvar"
	"maps"
	"net/http"
	"strconv"

	"github.com/vocdoni/davinci-node/config"
)

// info returns the information needed by the client to generate a ballot zkSNARK proof
// GET /info
func (a *API) info(w http.ResponseWriter, r *http.Request) {
	// Build the response with the necessary circuit information
	runtimes := make(map[uint64]SequencerRuntimeInfo, len(a.runtimeInfos))
	maps.Copy(runtimes, a.runtimeInfos)
	response := &SequencerInfo{
		CircuitURL:          config.BallotProofCircuitURL,
		CircuitHash:         config.BallotProofCircuitHash,
		ProvingKeyURL:       config.BallotProofProvingKeyURL,
		ProvingKeyHash:      config.BallotProofProvingKeyHash,
		VerificationKeyURL:  config.BallotProofVerificationKeyURL,
		VerificationKeyHash: config.BallotProofVerificationKeyHash,
		Runtimes:            runtimes,
	}
	// if the sequencer has a signer, include the sequencer address
	if a.sequencerSigner != nil {
		response.SequencerAddress = a.sequencerSigner.Address().Bytes()
	}
	httpWriteJSON(w, response)
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
