package web3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	npbindings "github.com/vocdoni/davinci-contracts/golang-types"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/types"
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

func TestProcessAtBlockUsesHistoricalSnapshot(t *testing.T) {
	c := qt.New(t)

	processID := testutil.RandomProcessID()
	historicalRoot := big.NewInt(11)
	latestRoot := big.NewInt(22)

	backend := &testProcessRegistryBackend{
		historical: npbindings.DAVINCITypesProcess{
			Status:                uint8(types.ProcessStatusReady),
			OrganizationId:        common.HexToAddress("0x1234567890123456789012345678901234567890"),
			EncryptionKey:         npbindings.DAVINCITypesEncryptionKey{X: big.NewInt(3), Y: big.NewInt(7)},
			LatestStateRoot:       historicalRoot,
			Result:                []*big.Int{},
			StartTime:             big.NewInt(1000),
			Duration:              big.NewInt(3600),
			MaxVoters:             big.NewInt(100),
			VotersCount:           big.NewInt(0),
			OverwrittenVotesCount: big.NewInt(0),
			CreationBlock:         big.NewInt(10),
			BatchNumber:           big.NewInt(0),
			MetadataURI:           "https://example.com/metadata",
			BallotMode:            npbindings.DAVINCITypesBallotMode{UniqueValues: false, NumFields: 5, GroupSize: 0, CostExponent: 2, MaxValue: big.NewInt(10), MinValue: big.NewInt(0), MaxValueSum: big.NewInt(100), MinValueSum: big.NewInt(0)},
			Census: npbindings.DAVINCITypesCensus{
				CensusOrigin:    uint8(types.CensusOriginMerkleTreeOffchainStaticV1),
				CensusRoot:      [32]byte{},
				ContractAddress: common.Address{},
				CensusURI:       "https://example.com/census",
			},
		},
		latest: npbindings.DAVINCITypesProcess{
			Status:                uint8(types.ProcessStatusReady),
			OrganizationId:        common.HexToAddress("0x1234567890123456789012345678901234567890"),
			EncryptionKey:         npbindings.DAVINCITypesEncryptionKey{X: big.NewInt(3), Y: big.NewInt(7)},
			LatestStateRoot:       latestRoot,
			Result:                []*big.Int{},
			StartTime:             big.NewInt(1000),
			Duration:              big.NewInt(3600),
			MaxVoters:             big.NewInt(100),
			VotersCount:           big.NewInt(7),
			OverwrittenVotesCount: big.NewInt(0),
			CreationBlock:         big.NewInt(10),
			BatchNumber:           big.NewInt(0),
			MetadataURI:           "https://example.com/metadata",
			BallotMode:            npbindings.DAVINCITypesBallotMode{UniqueValues: false, NumFields: 5, GroupSize: 0, CostExponent: 2, MaxValue: big.NewInt(10), MinValue: big.NewInt(0), MaxValueSum: big.NewInt(100), MinValueSum: big.NewInt(0)},
			Census: npbindings.DAVINCITypesCensus{
				CensusOrigin:    uint8(types.CensusOriginMerkleTreeOffchainStaticV1),
				CensusRoot:      [32]byte{},
				ContractAddress: common.Address{},
				CensusURI:       "https://example.com/census",
			},
		},
	}

	processes, err := npbindings.NewProcessRegistryCaller(common.HexToAddress("0x1"), backend)
	c.Assert(err, qt.IsNil)

	contracts := &Contracts{
		processes:              &npbindings.ProcessRegistry{ProcessRegistryCaller: *processes},
		currentBlock:           99,
		currentBlockLastUpdate: time.Now(),
		knownProcesses:         make(map[types.ProcessID]struct{}),
	}

	got, err := contracts.ProcessAtBlock(processID, 42)
	c.Assert(err, qt.IsNil)
	c.Assert(got.StateRoot.MathBigInt().Cmp(historicalRoot), qt.Equals, 0)
	c.Assert(got.VotersCount.MathBigInt().Cmp(big.NewInt(0)), qt.Equals, 0)
}

func TestProcessAtBlockUsesCreationSnapshot(t *testing.T) {
	c := qt.New(t)

	processID := testutil.RandomProcessID()
	creationRoot := big.NewInt(33)

	backend := &testProcessRegistryBackend{
		creation: npbindings.DAVINCITypesProcess{
			Status:                uint8(types.ProcessStatusReady),
			OrganizationId:        common.HexToAddress("0x1234567890123456789012345678901234567890"),
			EncryptionKey:         npbindings.DAVINCITypesEncryptionKey{X: big.NewInt(3), Y: big.NewInt(7)},
			LatestStateRoot:       creationRoot,
			Result:                []*big.Int{},
			StartTime:             big.NewInt(1000),
			Duration:              big.NewInt(3600),
			MaxVoters:             big.NewInt(100),
			VotersCount:           big.NewInt(0),
			OverwrittenVotesCount: big.NewInt(0),
			CreationBlock:         big.NewInt(10),
			BatchNumber:           big.NewInt(0),
			MetadataURI:           "https://example.com/metadata",
			BallotMode:            npbindings.DAVINCITypesBallotMode{UniqueValues: false, NumFields: 5, GroupSize: 0, CostExponent: 2, MaxValue: big.NewInt(10), MinValue: big.NewInt(0), MaxValueSum: big.NewInt(100), MinValueSum: big.NewInt(0)},
			Census: npbindings.DAVINCITypesCensus{
				CensusOrigin:    uint8(types.CensusOriginMerkleTreeOffchainStaticV1),
				CensusRoot:      [32]byte{},
				ContractAddress: common.Address{},
				CensusURI:       "https://example.com/census",
			},
		},
	}

	processes, err := npbindings.NewProcessRegistryCaller(common.HexToAddress("0x1"), backend)
	c.Assert(err, qt.IsNil)

	contracts := &Contracts{
		processes:              &npbindings.ProcessRegistry{ProcessRegistryCaller: *processes},
		currentBlock:           99,
		currentBlockLastUpdate: time.Now(),
		knownProcesses:         make(map[types.ProcessID]struct{}),
	}

	got, err := contracts.ProcessAtBlock(processID, 10)
	c.Assert(err, qt.IsNil)
	c.Assert(got.StateRoot.MathBigInt().Cmp(creationRoot), qt.Equals, 0)
	c.Assert(got.VotersCount.MathBigInt().Cmp(big.NewInt(0)), qt.Equals, 0)
}

type testProcessRegistryBackend struct {
	creation   npbindings.DAVINCITypesProcess
	historical npbindings.DAVINCITypesProcess
	latest     npbindings.DAVINCITypesProcess
}

func (b *testProcessRegistryBackend) CodeAt(context.Context, common.Address, *big.Int) ([]byte, error) {
	return []byte{0x1}, nil
}

func (b *testProcessRegistryBackend) CallContract(_ context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if call.To == nil {
		return nil, fmt.Errorf("missing contract address")
	}
	if !bytes.Equal(call.To.Bytes(), common.HexToAddress("0x1").Bytes()) {
		return nil, fmt.Errorf("unexpected contract address: %s", call.To.Hex())
	}
	process := b.latest
	if blockNumber != nil && blockNumber.Cmp(big.NewInt(10)) == 0 {
		process = b.creation
	}
	if blockNumber != nil && blockNumber.Cmp(big.NewInt(42)) == 0 {
		process = b.historical
	}
	packed, err := processRegistryABI.Methods["getProcess"].Outputs.Pack(process)
	if err != nil {
		return nil, err
	}
	return packed, nil
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
