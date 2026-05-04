package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/config"
)

func TestInfo(t *testing.T) {
	c := qt.New(t)

	api := &API{
		runtimeInfos: map[uint64]SequencerRuntimeInfo{
			11155111: {
				Network: "sepolia",
				Contracts: ContractAddresses{
					ProcessRegistry:           "0x1111111111111111111111111111111111111111",
					StateTransitionZKVerifier: "0x2222222222222222222222222222222222222222",
					ResultsZKVerifier:         "0x3333333333333333333333333333333333333333",
				},
			},
			42161: {
				Network: "arbitrum",
				Contracts: ContractAddresses{
					ProcessRegistry:           "0x4444444444444444444444444444444444444444",
					StateTransitionZKVerifier: "0x5555555555555555555555555555555555555555",
					ResultsZKVerifier:         "0x6666666666666666666666666666666666666666",
				},
			},
		},
	}

	req, err := http.NewRequest(http.MethodGet, InfoEndpoint, nil)
	c.Assert(err, qt.IsNil)

	rr := httptest.NewRecorder()
	api.info(rr, req)

	c.Assert(rr.Code, qt.Equals, http.StatusOK)

	var response SequencerInfo
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	c.Assert(err, qt.IsNil)

	c.Assert(response.CircuitURL, qt.Equals, config.BallotProofCircuitURL)
	c.Assert(response.CircuitHash, qt.Equals, config.BallotProofCircuitHash)
	c.Assert(response.ProvingKeyURL, qt.Equals, config.BallotProofProvingKeyURL)
	c.Assert(response.ProvingKeyHash, qt.Equals, config.BallotProofProvingKeyHash)
	c.Assert(response.VerificationKeyURL, qt.Equals, config.BallotProofVerificationKeyURL)
	c.Assert(response.VerificationKeyHash, qt.Equals, config.BallotProofVerificationKeyHash)
	c.Assert(response.Runtimes, qt.HasLen, 2)
	c.Assert(response.Runtimes[11155111].Network, qt.Equals, "sepolia")
	c.Assert(response.Runtimes[11155111].Contracts.ProcessRegistry, qt.Equals, "0x1111111111111111111111111111111111111111")
	c.Assert(response.Runtimes[42161].Network, qt.Equals, "arbitrum")
	c.Assert(response.Runtimes[42161].Contracts.ResultsZKVerifier, qt.Equals, "0x6666666666666666666666666666666666666666")
}
