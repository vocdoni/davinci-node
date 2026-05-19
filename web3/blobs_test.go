package web3

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	qt "github.com/frankban/quicktest"
	"github.com/holiman/uint256"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3/rpc"
)

func TestBlobsByTxHashUsesExecutionBlockTimeSlot(t *testing.T) {
	c := qt.New(t)

	const (
		chainID       = uint64(1)
		executionSlot = uint64(102)
	)

	genesisTime := time.Unix(1_700_000_000, 0).UTC()
	slotDuration := 12 * time.Second
	executionTime := genesisTime.Add(time.Duration(executionSlot) * slotDuration)
	executionBlockTime := uint64(executionTime.Unix())

	parentBeaconRoot := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	executionBlockHash := common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	parentHash := common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333")

	var blobCommitment deneb.KZGCommitment
	blobCommitmentHash := types.KZGCommitment(blobCommitment)
	blobHashBytes := blobCommitmentHash.CalcBlobHashV1(sha256.New())
	blobHash := common.BytesToHash(blobHashBytes[:])

	tx := gethtypes.NewTx(&gethtypes.BlobTx{
		ChainID:    uint256.NewInt(chainID),
		Nonce:      7,
		GasTipCap:  uint256.NewInt(1),
		GasFeeCap:  uint256.NewInt(2),
		Gas:        21000,
		To:         common.HexToAddress("0x4444444444444444444444444444444444444444"),
		Value:      uint256.NewInt(0),
		Data:       []byte{},
		BlobFeeCap: uint256.NewInt(1),
		BlobHashes: []common.Hash{blobHash},
		V:          uint256.NewInt(0),
		R:          uint256.NewInt(1),
		S:          uint256.NewInt(1),
	})
	txJSON, err := json.Marshal(tx)
	c.Assert(err, qt.IsNil)
	txHash := tx.Hash()

	executionHeader := newTestHeader(parentHash, 102, executionBlockTime, &parentBeaconRoot)
	executionHeaderJSON := mustJSONMap(c, executionHeader)
	executionBlockJSON := mustJSONMap(c, executionHeader)
	executionBlockJSON["transactions"] = []any{}

	var proof deneb.KZGCommitmentInclusionProof
	sidecar := &deneb.BlobSidecar{
		Index:         0,
		Blob:          deneb.Blob{},
		KZGCommitment: blobCommitment,
		KZGProof:      deneb.KZGProof{},
		SignedBlockHeader: &phase0.SignedBeaconBlockHeader{
			Message: &phase0.BeaconBlockHeader{
				Slot:          phase0.Slot(executionSlot),
				ProposerIndex: 0,
				ParentRoot:    phase0.Root{},
				StateRoot:     phase0.Root{},
				BodyRoot:      phase0.Root{},
			},
			Signature: phase0.BLSSignature{},
		},
		KZGCommitmentInclusionProof: proof,
	}
	sidecarJSON := mustJSON(c, sidecar)

	var requestedSlotsMu sync.Mutex
	requestedSlots := make([]uint64, 0, 1)
	var countsMu sync.Mutex
	genesisCalls := 0
	specCalls := 0
	sidecarCalls := 0

	beaconSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/eth/v1/config/spec":
			countsMu.Lock()
			specCalls++
			countsMu.Unlock()
			writeTestJSON(w, http.StatusOK, map[string]any{
				"data": map[string]any{
					"SECONDS_PER_SLOT": "12",
				},
			})
		case r.URL.Path == "/eth/v1/beacon/genesis":
			countsMu.Lock()
			genesisCalls++
			countsMu.Unlock()
			writeTestJSON(w, http.StatusOK, map[string]any{
				"data": &v1.Genesis{
					GenesisTime:           genesisTime,
					GenesisValidatorsRoot: phase0.Root{},
					GenesisForkVersion:    phase0.Version{},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/eth/v1/beacon/blob_sidecars/"):
			countsMu.Lock()
			sidecarCalls++
			countsMu.Unlock()
			slotStr := strings.TrimPrefix(r.URL.Path, "/eth/v1/beacon/blob_sidecars/")
			slot, err := strconv.ParseUint(slotStr, 10, 64)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			requestedSlotsMu.Lock()
			requestedSlots = append(requestedSlots, slot)
			requestedSlotsMu.Unlock()

			if slot != executionSlot {
				http.NotFound(w, r)
				return
			}

			writeTestJSON(w, http.StatusOK, map[string]any{
				"data": []json.RawMessage{json.RawMessage(sidecarJSON)},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	c.Cleanup(beaconSrv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	elSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			_ = r.Body.Close()
		}()

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
			resp.Result = executionBlockJSON
		case "eth_getBlockTransactionCountByHash":
			resp.Result = "0x0"
		case "eth_getTransactionReceipt":
			resp.Result = map[string]any{
				"blockHash":         executionBlockHash.Hex(),
				"blockNumber":       "0x66",
				"contractAddress":   nil,
				"cumulativeGasUsed": "0x5208",
				"effectiveGasPrice": "0x1",
				"from":              common.Address{}.Hex(),
				"gasUsed":           "0x5208",
				"logs":              []any{},
				"logsBloom":         "0x" + strings.Repeat("0", 512),
				"status":            "0x1",
				"to":                common.Address{}.Hex(),
				"transactionHash":   txHash.Hex(),
				"transactionIndex":  "0x0",
				"type":              "0x3",
			}
		case "eth_getTransactionByHash":
			resp.Result = json.RawMessage(txJSON)
		case "eth_getBlockByHash":
			resp.Result = executionHeaderJSON
		default:
			resp.Error = &testRPCError{
				Code:    -32601,
				Message: "method not found",
			}
		}

		writeTestJSON(w, http.StatusOK, resp)
	}))
	c.Cleanup(elSrv.Close)

	pool := rpc.NewWeb3Pool()
	addedChainID, err := pool.AddEndpoint(elSrv.URL)
	c.Assert(err, qt.IsNil)
	c.Assert(addedChainID, qt.Equals, chainID)

	cli, err := pool.Client(addedChainID)
	c.Assert(err, qt.IsNil)

	contracts := &Contracts{
		ChainID:                  addedChainID,
		web3pool:                 pool,
		cli:                      cli,
		Web3ConsensusAPIEndpoint: beaconSrv.URL,
		supportForBlobTxs:        true,
	}
	c.Assert(contracts.initBeaconTiming(ctx), qt.IsNil)

	sidecars, err := contracts.BlobsByTxHash(ctx, txHash)
	c.Assert(err, qt.IsNil)
	c.Assert(sidecars, qt.HasLen, 1)
	c.Assert(sidecars[0].VersionedBlobHash(), qt.Equals, blobHash)

	sidecars, err = contracts.BlobsByTxHash(ctx, txHash)
	c.Assert(err, qt.IsNil)
	c.Assert(sidecars, qt.HasLen, 1)
	c.Assert(sidecars[0].VersionedBlobHash(), qt.Equals, blobHash)

	requestedSlotsMu.Lock()
	defer requestedSlotsMu.Unlock()
	c.Assert(requestedSlots, qt.DeepEquals, []uint64{executionSlot, executionSlot})

	countsMu.Lock()
	defer countsMu.Unlock()
	c.Assert(genesisCalls, qt.Equals, 1)
	c.Assert(specCalls, qt.Equals, 1)
	c.Assert(sidecarCalls, qt.Equals, 2)
}

func newTestHeader(parentHash common.Hash, number uint64, blockTime uint64, parentBeaconRoot *common.Hash) *gethtypes.Header {
	header := &gethtypes.Header{
		ParentHash:  parentHash,
		UncleHash:   gethtypes.EmptyUncleHash,
		Coinbase:    common.Address{},
		Root:        common.Hash{},
		TxHash:      gethtypes.EmptyTxsHash,
		ReceiptHash: gethtypes.EmptyReceiptsHash,
		Bloom:       gethtypes.Bloom{},
		Difficulty:  big.NewInt(0),
		Number:      new(big.Int).SetUint64(number),
		GasLimit:    30_000_000,
		GasUsed:     0,
		Time:        blockTime,
		Extra:       []byte{},
		MixDigest:   common.Hash{},
		Nonce:       gethtypes.BlockNonce{},
		BaseFee:     big.NewInt(0),
	}

	if parentBeaconRoot != nil {
		header.ParentBeaconRoot = parentBeaconRoot
	}

	blobGasUsed := uint64(0)
	excessBlobGas := uint64(0)
	header.BlobGasUsed = &blobGasUsed
	header.ExcessBlobGas = &excessBlobGas

	return header
}

func mustJSONMap(c *qt.C, value any) map[string]any {
	raw := mustJSON(c, value)
	var out map[string]any
	c.Assert(json.Unmarshal(raw, &out), qt.IsNil)
	return out
}

func mustJSON(c *qt.C, value any) []byte {
	raw, err := json.Marshal(value)
	c.Assert(err, qt.IsNil)
	return raw
}

func writeTestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
