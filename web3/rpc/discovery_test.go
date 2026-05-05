package rpc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	qt "github.com/frankban/quicktest"
)

type testRPCRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
}

type testRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result"`
	Error   *testRPCError   `json:"error,omitempty"`
}

type testRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func TestGroupEndpointsByChainID(t *testing.T) {
	c := qt.New(t)

	sepoliaRPCOne := testRPCServerForChainID(11155111)
	c.Cleanup(sepoliaRPCOne.Close)
	sepoliaRPCTwo := testRPCServerForChainID(11155111)
	c.Cleanup(sepoliaRPCTwo.Close)
	celoRPC := testRPCServerForChainID(42220)
	c.Cleanup(celoRPC.Close)

	grouped, err := GroupEndpointsByChainID([]string{
		sepoliaRPCOne.URL,
		celoRPC.URL,
		sepoliaRPCTwo.URL,
	})

	c.Assert(err, qt.IsNil)
	c.Assert(grouped, qt.DeepEquals, map[uint64][]string{
		11155111: {sepoliaRPCOne.URL, sepoliaRPCTwo.URL},
		42220:    {celoRPC.URL},
	})
}

func TestEndpointsForChainID(t *testing.T) {
	c := qt.New(t)

	sepoliaRPC := testRPCServerForChainID(11155111)
	c.Cleanup(sepoliaRPC.Close)
	sepoliaRPCTwo := testRPCServerForChainID(11155111)
	c.Cleanup(sepoliaRPCTwo.Close)

	endpoints, err := EndpointsForChainID([]string{sepoliaRPC.URL, sepoliaRPCTwo.URL}, 11155111)

	c.Assert(err, qt.IsNil)
	c.Assert(endpoints, qt.DeepEquals, []string{sepoliaRPC.URL, sepoliaRPCTwo.URL})
}

func TestEndpointsForChainIDRejectsMixedChains(t *testing.T) {
	c := qt.New(t)

	sepoliaRPC := testRPCServerForChainID(11155111)
	c.Cleanup(sepoliaRPC.Close)
	celoRPC := testRPCServerForChainID(42220)
	c.Cleanup(celoRPC.Close)

	endpoints, err := EndpointsForChainID([]string{sepoliaRPC.URL, celoRPC.URL}, 11155111)

	c.Assert(endpoints, qt.IsNil)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "unexpected chain IDs [42220] for expected chain ID 11155111")
}

func testRPCServerForChainID(chainID uint64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()

		var req testRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp := testRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
		}

		switch req.Method {
		case "eth_chainId":
			resp.Result = fmt.Sprintf("0x%x", chainID)
		case "eth_getBlockByNumber":
			resp.Result = map[string]any{
				"hash":         "0x1",
				"number":       "0x1",
				"transactions": []any{},
			}
		case "eth_getBlockTransactionCountByHash":
			resp.Result = "0x0"
		default:
			resp.Error = &testRPCError{
				Code:    -32601,
				Message: "method not found",
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
}
