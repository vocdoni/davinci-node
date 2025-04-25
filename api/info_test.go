package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/vocdoni-z-sandbox/config"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
)

func TestInfo(t *testing.T) {
	c := qt.New(t)

	// Create a mock storage
	store := &storage.Storage{}

	// Test case 1: Valid network
	t.Run("ValidNetwork", func(t *testing.T) {
		// Create API with a valid network
		api := &API{
			storage: store,
			network: "sep", // This is a valid network as defined in config.DefaultConfig
		}

		// Create a new request
		req, err := http.NewRequest("GET", InfoEndpoint, nil)
		c.Assert(err, qt.IsNil)

		// Create a response recorder to record the response
		rr := httptest.NewRecorder()

		// Call the handler
		api.info(rr, req)

		// Check the status code
		c.Assert(rr.Code, qt.Equals, http.StatusOK)

		// Parse the response
		var response BallotProofInfo
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		c.Assert(err, qt.IsNil)

		// Verify the returned data
		c.Assert(response.CircuitURL, qt.Equals, config.BallotProoCircuitURL)
		c.Assert(response.CircuitHash, qt.Equals, config.BallotProofCircuitHash)
		c.Assert(response.ProvingKeyURL, qt.Equals, config.BallotProofProvingKeyURL)
		c.Assert(response.ProvingKeyHash, qt.Equals, config.BallotProofProvingKeyHash)
		c.Assert(response.VerificationKeyURL, qt.Equals, config.BallotProofVerificationKeyURL)
		c.Assert(response.VerificationKeyHash, qt.Equals, config.BallotProofVerificationKeyHash)

		// Check contract addresses
		contracts := config.DefaultConfig["sep"]
		c.Assert(response.Contracts.ProcessRegistry, qt.Equals, contracts.ProcessRegistrySmartContract)
		c.Assert(response.Contracts.OrganizationRegistry, qt.Equals, contracts.OrganizationRegistrySmartContract)
		c.Assert(response.Contracts.Results, qt.Equals, contracts.ResultsSmartContract)
	})

	// Test case 2: Invalid network
	t.Run("InvalidNetwork", func(t *testing.T) {
		// Create API with an invalid network
		api := &API{
			storage: store,
			network: "invalid_network", // This network doesn't exist in config.DefaultConfig
		}

		// Create a new request
		req, err := http.NewRequest("GET", InfoEndpoint, nil)
		c.Assert(err, qt.IsNil)

		// Create a response recorder to record the response
		rr := httptest.NewRecorder()

		// Call the handler
		api.info(rr, req)

		// Check the status code (should be an error)
		c.Assert(rr.Code, qt.Equals, http.StatusInternalServerError)

		// Verify the error message contains the expected content
		c.Assert(rr.Body.String(), qt.Contains, "invalid network configuration")
	})
}
