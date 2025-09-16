package web3

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gtypes "github.com/ethereum/go-ethereum/core/types"
)

// SimulationRequest is the top‐level payload for eth_simulateV1
type SimulationRequest struct {
	BlockStateCalls        []BlockStateCall `json:"blockStateCalls"`
	Validation             bool             `json:"validation,omitempty"`
	TraceTransfers         bool             `json:"traceTransfers,omitempty"`
	ReturnFullTransactions bool             `json:"returnFullTransactions,omitempty"`
}

// BlockStateCall lets you override block fields, state, and queue up calls
type BlockStateCall struct {
	BlockOverrides *BlockOverrides          `json:"blockOverrides,omitempty"`
	StateOverrides map[string]StateOverride `json:"stateOverrides,omitempty"`
	Calls          []Call                   `json:"calls"`
}

// BlockOverrides lets you override block fields
type BlockOverrides struct {
	BaseFeePerGas *hexutil.Big `json:"baseFeePerGas,omitempty"`
	Timestamp     *hexutil.Big `json:"timestamp,omitempty"`
}

// StateOverride lets you override state fields
type StateOverride struct {
	Balance *hexutil.Big   `json:"balance,omitempty"`
	Nonce   hexutil.Uint64 `json:"nonce,omitempty"`
	Code    *hexutil.Bytes `json:"code,omitempty"`
}

// Call is a single call to be executed in the simulated block
type Call struct {
	From                 common.Address        `json:"from,omitempty"`
	To                   common.Address        `json:"to,omitempty"`
	Gas                  hexutil.Uint64        `json:"gas,omitempty"`
	GasPrice             *hexutil.Big          `json:"gasPrice,omitempty"`
	MaxFeePerGas         *hexutil.Big          `json:"maxFeePerGas,omitempty"`
	MaxPriorityFeePerGas *hexutil.Big          `json:"maxPriorityFeePerGas,omitempty"`
	Value                *hexutil.Big          `json:"value,omitempty"`
	Data                 hexutil.Bytes         `json:"data,omitempty"`
	Nonce                hexutil.Uint64        `json:"nonce,omitempty"`
	BlobHashes           []common.Hash         `json:"blobHashes,omitempty"`
	Sidecar              *gtypes.BlobTxSidecar `json:"sidecar,omitempty"`
}

// SimulatedBlock is the result of a simulated block
type SimulatedBlock struct {
	Hash   common.Hash  `json:"hash"`
	Number string       `json:"number"`
	Calls  []CallResult `json:"calls"`
}

// CallResult is the result of a single call in a simulated block
type CallResult struct {
	Status     string         `json:"status"` // "0x1" or "0x0"
	ReturnData hexutil.Bytes  `json:"returnData"`
	GasUsed    hexutil.Uint64 `json:"gasUsed"`
	Logs       []interface{}  `json:"logs"`
	Error      *RPCError      `json:"error,omitempty"`
}

// RPCError is the error returned by the RPC server
type RPCError struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    hexutil.Bytes `json:"data"`
}
