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
		networksInfo: map[uint64]SequencerNetworkInfo{
			11155111: {
				ChainID:                 11155111,
				ProcessRegistryContract: "0x1111111111111111111111111111111111111111",
			},
			42161: {
				ChainID:                 42161,
				ProcessRegistryContract: "0x4444444444444444444444444444444444444444",
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
	c.Assert(response.Networks, qt.HasLen, 2)
	c.Assert(response.Networks[11155111].ChainID, qt.Equals, uint64(11155111))
	c.Assert(response.Networks[11155111].ProcessRegistryContract, qt.Equals, "0x1111111111111111111111111111111111111111")
	c.Assert(response.Networks[42161].ChainID, qt.Equals, uint64(42161))
	c.Assert(response.Networks[42161].ProcessRegistryContract, qt.Equals, "0x4444444444444444444444444444444444444444")
}
