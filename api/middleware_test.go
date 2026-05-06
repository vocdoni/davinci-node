package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/go-chi/chi/v5"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

func TestSkipUnknownProcessIDMiddleware(t *testing.T) {
	c := qt.New(t)

	sepoliaRuntime := testAPIRuntime(c, 11155111, common.HexToAddress("0x0000000000000000000000000000000000000001"))
	arbitrumRuntime := testAPIRuntime(c, 42161, common.HexToAddress("0x0000000000000000000000000000000000000002"))
	versionUnknown := [4]byte{0x09, 0x0a, 0x0b, 0x0c}
	routerRuntime, err := web3.NewRuntimeRouter(
		sepoliaRuntime,
		arbitrumRuntime,
	)
	c.Assert(err, qt.IsNil)

	router := chi.NewRouter()
	router.With(skipUnknownProcessIDMiddleware(routerRuntime)).Get(ProcessEndpoint, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	testCases := []struct {
		name      string
		processID types.ProcessID
		supported bool
		wantCode  int
	}{
		{
			name:      "accepted first version",
			processID: types.NewProcessID(common.HexToAddress("0x0000000000000000000000000000000000000004"), sepoliaRuntime.ProcessIDVersion, 1),
			supported: true,
			wantCode:  http.StatusOK,
		},
		{
			name:      "accepted second version",
			processID: types.NewProcessID(common.HexToAddress("0x0000000000000000000000000000000000000005"), arbitrumRuntime.ProcessIDVersion, 2),
			supported: true,
			wantCode:  http.StatusOK,
		},
		{
			name:      "rejected unknown version",
			processID: types.NewProcessID(common.HexToAddress("0x0000000000000000000000000000000000000006"), versionUnknown, 3),
			supported: false,
			wantCode:  http.StatusNotFound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := qt.New(t)
			c.Assert(routerRuntime.SupportsProcess(tc.processID), qt.Equals, tc.supported)
			req := httptest.NewRequest(http.MethodGet, "/processes/"+tc.processID.String(), nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			c.Assert(rr.Code, qt.Equals, tc.wantCode)
		})
	}
}

func TestLoggingMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})

	wrappedHandler := loggingMiddleware(100)(handler)

	testCases := []struct {
		name string
		body string
	}{
		{name: "json object", body: `{"key":"value"}`},
		{name: "json array", body: `[1,2,3]`},
		{name: "json with whitespace", body: `  {"key":"value"}`},
		{name: "binary data", body: "\x00\x01\x02\x03\x04"},
		{name: "plain text", body: "Hello, World!"},
		{name: "empty body", body: ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := qt.New(t)
			req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rec, req)

			c.Assert(rec.Code, qt.Equals, http.StatusOK)
			respBody, err := io.ReadAll(rec.Body)
			c.Assert(err, qt.IsNil)
			c.Assert(string(respBody), qt.Equals, tc.body)
		})
	}
}

func TestLoggingMiddlewareExclusions(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrappedHandler := loggingMiddleware(100)(handler)

	testCases := []struct {
		name string
		path string
	}{
		{name: "ping endpoint", path: "/ping"},
		{name: "workers endpoint", path: "/workers/123/job"},
		{name: "regular endpoint", path: "/votes"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := qt.New(t)
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rec, req)

			c.Assert(rec.Code, qt.Equals, http.StatusOK)
		})
	}
}

func TestLoggingConfigCustomExclusions(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	config := LoggingConfig{
		MaxBodyLog: 100,
		ExcludedPrefixes: []string{
			"/api/v1/",
			"/health",
			"/metrics",
		},
	}
	wrappedHandler := loggingMiddlewareWithConfig(config)(handler)

	testCases := []struct {
		path string
	}{
		{path: "/api/v1/users"},
		{path: "/api/v1/posts/123"},
		{path: "/health"},
		{path: "/healthcheck"},
		{path: "/metrics/prometheus"},
		{path: "/api/v2/users"},
		{path: "/users"},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			c := qt.New(t)
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rec, req)

			c.Assert(rec.Code, qt.Equals, http.StatusOK)
		})
	}
}

func TestResponseWriterCapture(t *testing.T) {
	testCases := []struct {
		name           string
		handlerFunc    func(w http.ResponseWriter, r *http.Request)
		expectedStatus int
	}{
		{
			name: "WriteHeader before Write",
			handlerFunc: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("test"))
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "Write without WriteHeader",
			handlerFunc: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("test"))
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "Multiple WriteHeader calls",
			handlerFunc: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				w.WriteHeader(http.StatusAccepted)
			},
			expectedStatus: http.StatusCreated,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := qt.New(t)
			rec := httptest.NewRecorder()
			rw := &responseWriter{
				ResponseWriter: rec,
				statusCode:     0,
			}

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tc.handlerFunc(rw, req)

			c.Assert(rw.statusCode, qt.Equals, tc.expectedStatus)
		})
	}
}

func TestAPIRuntimeDataResolvesMissingVerifierAddresses(t *testing.T) {
	c := qt.New(t)

	contracts := &web3.Contracts{
		ChainID: 11155111,
		ContractsAddresses: &web3.Addresses{
			ProcessRegistry: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		},
	}
	runtime, err := web3.NewNetworkRuntime(contracts, nil)
	c.Assert(err, qt.IsNil)
	router, err := web3.NewRuntimeRouter(runtime)
	c.Assert(err, qt.IsNil)

	runtimeInfos, err := apiRuntimeData(router)
	c.Assert(err, qt.IsNil)

	info, ok := runtimeInfos[11155111]
	c.Assert(ok, qt.IsTrue)
	c.Assert(info.ChainID, qt.Equals, uint64(11155111))
	c.Assert(info.ProcessRegistryContract, qt.Equals, "0x1111111111111111111111111111111111111111")
}

func testAPIRuntime(c *qt.C, chainID uint64, processRegistry common.Address) *web3.NetworkRuntime {
	runtime, err := web3.NewNetworkRuntime(&web3.Contracts{
		ChainID: chainID,
		ContractsAddresses: &web3.Addresses{
			ProcessRegistry: processRegistry,
		},
	}, nil)
	c.Assert(err, qt.IsNil)
	return runtime
}

func BenchmarkLoggingMiddleware(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := loggingMiddleware(512)(handler)
	jsonBody := `{"key": "value", "number": 123, "array": [1, 2, 3]}`

	b.Run("JSON body", func(b *testing.B) {
		for b.Loop() {
			req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(jsonBody))
			rec := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(rec, req)
		}
	})

	b.Run("Binary body", func(b *testing.B) {
		binaryBody := bytes.Repeat([]byte{0x00, 0x01, 0x02, 0x03}, 100)
		for b.Loop() {
			req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(binaryBody))
			rec := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(rec, req)
		}
	})

	b.Run("No body", func(b *testing.B) {
		for b.Loop() {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(rec, req)
		}
	})
}
