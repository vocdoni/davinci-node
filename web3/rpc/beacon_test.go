package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	qt "github.com/frankban/quicktest"
)

// testBeaconSpecResponse represents a valid beacon spec JSON response.
type testBeaconSpecResponse struct {
	Data testBeaconSpecData `json:"data"`
}

type testBeaconSpecData struct {
	DepositNetworkID string `json:"DEPOSIT_NETWORK_ID"`
}

// testBeaconSpecServer creates a test HTTP server that responds to
// /eth/v1/config/spec with the given chain ID.
func testBeaconSpecServer(chainID uint64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eth/v1/config/spec" {
			http.NotFound(w, r)
			return
		}

		resp := testBeaconSpecResponse{
			Data: testBeaconSpecData{
				DepositNetworkID: fmt.Sprintf("%d", chainID),
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestBeaconChainID(t *testing.T) {
	t.Run("valid response", func(t *testing.T) {
		c := qt.New(t)
		expectedChainID := uint64(1)
		srv := testBeaconSpecServer(expectedChainID)
		c.Cleanup(srv.Close)

		chainID, err := BeaconChainID(t.Context(), srv.URL)
		c.Assert(err, qt.IsNil)
		c.Assert(chainID, qt.Equals, expectedChainID)
	})

	t.Run("different chain ID", func(t *testing.T) {
		c := qt.New(t)
		expectedChainID := uint64(11155111)
		srv := testBeaconSpecServer(expectedChainID)
		c.Cleanup(srv.Close)

		chainID, err := BeaconChainID(t.Context(), srv.URL)
		c.Assert(err, qt.IsNil)
		c.Assert(chainID, qt.Equals, expectedChainID)
	})

	t.Run("trailing slash stripped", func(t *testing.T) {
		c := qt.New(t)
		expectedChainID := uint64(5)
		srv := testBeaconSpecServer(expectedChainID)
		c.Cleanup(srv.Close)

		chainID, err := BeaconChainID(t.Context(), srv.URL+"/")
		c.Assert(err, qt.IsNil)
		c.Assert(chainID, qt.Equals, expectedChainID)
	})

	t.Run("empty endpoint", func(t *testing.T) {
		_, err := BeaconChainID(t.Context(), "")
		qt.Assert(t, err, qt.Not(qt.IsNil))
		qt.Assert(t, err.Error(), qt.Contains, "empty")
	})
}

func TestBeaconChainIDErrors(t *testing.T) {
	t.Run("invalid JSON response", func(t *testing.T) {
		c := qt.New(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid json`))
		}))
		c.Cleanup(srv.Close)

		_, err := BeaconChainID(t.Context(), srv.URL)
		c.Assert(err, qt.Not(qt.IsNil))
	})

	t.Run("missing data field", func(t *testing.T) {
		c := qt.New(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}))
		c.Cleanup(srv.Close)

		_, err := BeaconChainID(t.Context(), srv.URL)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "no data")
	})

	t.Run("empty DEPOSIT_NETWORK_ID", func(t *testing.T) {
		c := qt.New(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := testBeaconSpecResponse{
				Data: testBeaconSpecData{
					DepositNetworkID: "",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		c.Cleanup(srv.Close)

		_, err := BeaconChainID(t.Context(), srv.URL)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "empty DEPOSIT_NETWORK_ID")
	})

	t.Run("non-numeric DEPOSIT_NETWORK_ID", func(t *testing.T) {
		c := qt.New(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := testBeaconSpecResponse{
				Data: testBeaconSpecData{
					DepositNetworkID: "not-a-number",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		c.Cleanup(srv.Close)

		_, err := BeaconChainID(t.Context(), srv.URL)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "parse")
	})

	t.Run("server returns error status", func(t *testing.T) {
		c := qt.New(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		}))
		c.Cleanup(srv.Close)

		_, err := BeaconChainID(t.Context(), srv.URL)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "status 404")
	})

	t.Run("server unavailable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		srv.Close()

		_, err := BeaconChainID(t.Context(), srv.URL)
		qt.Assert(t, err, qt.Not(qt.IsNil))
	})
	t.Run("cancelled context", func(t *testing.T) {
		c := qt.New(t)
		expectedChainID := uint64(1)
		srv := testBeaconSpecServer(expectedChainID)
		c.Cleanup(srv.Close)

		cancelCtx, cancelFn := context.WithCancel(t.Context())
		cancelFn()

		_, err := BeaconChainID(cancelCtx, srv.URL)
		c.Assert(err, qt.Not(qt.IsNil))
	})
}
