package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
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
// beaconConfigSpecPath with the given chain ID.
func testBeaconSpecServer(chainID uint64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != beaconConfigSpecPath {
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

func TestBeaconTimingRejectsOversizedSlotSeconds(t *testing.T) {
	c := qt.New(t)
	genesisTime := time.Unix(1_700_000_000, 0).UTC()
	genesisBody, err := json.Marshal(map[string]any{
		"data": &v1.Genesis{
			GenesisTime:           genesisTime,
			GenesisValidatorsRoot: phase0.Root{},
			GenesisForkVersion:    phase0.Version{},
		},
	})
	c.Assert(err, qt.IsNil)

	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/eth/v1/beacon/genesis":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(string(genesisBody))),
				Request:    req,
			}, nil
		case beaconConfigSpecPath:
			body = `{"data":{"SECONDS_PER_SLOT":"9223372036854775808"}}`
		default:
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("not found")),
				Request:    req,
			}, nil
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
	c.Cleanup(func() {
		http.DefaultTransport = oldTransport
	})

	_, _, err = BeaconTiming(t.Context(), "http://beacon.test")
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "parse SECONDS_PER_SLOT")
}

func TestBeaconTimingIgnoresBlobScheduleArrays(t *testing.T) {
	c := qt.New(t)
	genesisTime := time.Unix(1_700_000_000, 0).UTC()
	genesisBody, err := json.Marshal(map[string]any{
		"data": &v1.Genesis{
			GenesisTime:           genesisTime,
			GenesisValidatorsRoot: phase0.Root{},
			GenesisForkVersion:    phase0.Version{},
		},
	})
	c.Assert(err, qt.IsNil)

	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/eth/v1/beacon/genesis":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(string(genesisBody))),
				Request:    req,
			}, nil
		case beaconConfigSpecPath:
			body = `{"data":{"SECONDS_PER_SLOT":"12","BLOB_SCHEDULE":[{"EPOCH":"269568","MAX_BLOBS_PER_BLOCK":"6"}]}}`
		default:
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("not found")),
				Request:    req,
			}, nil
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
	c.Cleanup(func() {
		http.DefaultTransport = oldTransport
	})

	genesisUnix, slotDuration, err := BeaconTiming(t.Context(), "http://beacon.test")
	c.Assert(err, qt.IsNil)
	c.Assert(genesisUnix, qt.Equals, uint64(genesisTime.Unix()))
	c.Assert(slotDuration, qt.Equals, 12*time.Second)
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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
