package web3

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/web3/rpc"
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

func TestCheckTxStatus(t *testing.T) {
	c := qt.New(t)
	txHash := common.HexToHash("0x1234")

	c.Run("pending transaction", func(c *qt.C) {
		// invalid status to mark as no error
		contracts := testContractsForReceipt(c, txHash, 99999999999999)

		status, err := contracts.CheckTxStatus(txHash)

		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.IsFalse)
	})

	c.Run("successful transaction", func(c *qt.C) {
		contracts := testContractsForReceipt(c, txHash, 1)

		status, err := contracts.CheckTxStatus(txHash)

		c.Assert(err, qt.IsNil)
		c.Assert(status, qt.IsTrue)
	})

	c.Run("reverted transaction", func(c *qt.C) {
		contracts := testContractsForReceipt(c, txHash, 0)

		status, err := contracts.CheckTxStatus(txHash)

		c.Assert(status, qt.IsFalse)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "reverted")
	})
}

func TestWaitTxByHashReturnsOnRevert(t *testing.T) {
	c := qt.New(t)
	txHash := common.HexToHash("0x5678")
	contracts := testContractsForReceipt(c, txHash, 0)

	start := time.Now()
	err := contracts.waitTx(txHash, 1500*time.Millisecond)

	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "reverted")
	c.Assert(time.Since(start) < 1500*time.Millisecond, qt.IsTrue)
}

func testContractsForReceipt(c *qt.C, txHash common.Hash, receiptStatus uint64) *Contracts {
	server := testRPCServer(txHash, receiptStatus)
	c.Cleanup(server.Close)

	pool := rpc.NewWeb3Pool()
	chainID, err := pool.AddEndpoint(server.URL)
	c.Assert(err, qt.IsNil)

	client, err := pool.Client(chainID)
	c.Assert(err, qt.IsNil)

	return &Contracts{
		ChainID:  chainID,
		web3pool: pool,
		cli:      client,
	}
}

func testRPCServer(txHash common.Hash, receiptStatus uint64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

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
			resp.Result = "0x1"
		case "eth_getBlockByNumber":
			resp.Result = map[string]any{
				"hash":         common.HexToHash("0x1").Hex(),
				"number":       "0x1",
				"transactions": []any{},
			}
		case "eth_getBlockTransactionCountByHash":
			resp.Result = "0x0"
		case "eth_getTransactionReceipt":
			if receiptStatus == 99999999999999 {
				resp.Result = nil
				break
			}
			resp.Result = map[string]any{
				"blockHash":         common.HexToHash("0x2").Hex(),
				"blockNumber":       "0x1",
				"contractAddress":   nil,
				"cumulativeGasUsed": "0x5208",
				"effectiveGasPrice": "0x1",
				"from":              common.Address{}.Hex(),
				"gasUsed":           "0x5208",
				"logs":              []any{},
				"logsBloom":         "0x" + strings.Repeat("0", 512),
				"status":            fmt.Sprintf("0x%x", receiptStatus),
				"to":                common.Address{}.Hex(),
				"transactionHash":   txHash.Hex(),
				"transactionIndex":  "0x0",
				"type":              "0x2",
			}
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
