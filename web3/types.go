package web3

import (
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	npbindings "github.com/vocdoni/davinci-contracts/golang-types"
	"github.com/vocdoni/davinci-node/types"
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
	From                 common.Address           `json:"from,omitempty"`
	To                   common.Address           `json:"to,omitempty"`
	Gas                  hexutil.Uint64           `json:"gas,omitempty"`
	GasPrice             *hexutil.Big             `json:"gasPrice,omitempty"`
	MaxFeePerGas         *hexutil.Big             `json:"maxFeePerGas,omitempty"`
	MaxPriorityFeePerGas *hexutil.Big             `json:"maxPriorityFeePerGas,omitempty"`
	Value                *hexutil.Big             `json:"value,omitempty"`
	Data                 hexutil.Bytes            `json:"data,omitempty"`
	Nonce                hexutil.Uint64           `json:"nonce,omitempty"`
	BlobHashes           []common.Hash            `json:"blobHashes,omitempty"`
	Sidecar              *gethtypes.BlobTxSidecar `json:"sidecar,omitempty"`
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

// contractProcess is a mirror of the on-chain process tuple constructed
// with the auto-generated bindings.
type contractProcess struct {
	Status                uint8
	OrganizationId        common.Address
	EncryptionKey         npbindings.IProcessRegistryEncryptionKey
	LatestStateRoot       *big.Int
	StartTime             *big.Int
	Duration              *big.Int
	MaxVoters             *big.Int
	VotersCount           *big.Int
	OverwrittenVotesCount *big.Int
	MetadataURI           string
	BallotMode            npbindings.IProcessRegistryBallotMode
	Census                npbindings.IProcessRegistryCensus
	Result                []*big.Int
}

// contractProcess2Process converts a contractProcess to a types.Process
func contractProcess2Process(p *contractProcess) (*types.Process, error) {
	mode := types.BallotMode{
		UniqueValues:   p.BallotMode.UniqueValues,
		CostFromWeight: p.BallotMode.CostFromWeight,
		NumFields:      p.BallotMode.NumFields,
		CostExponent:   p.BallotMode.CostExponent,
		MaxValue:       (*types.BigInt)(p.BallotMode.MaxValue),
		MinValue:       (*types.BigInt)(p.BallotMode.MinValue),
		MaxValueSum:    (*types.BigInt)(p.BallotMode.MaxValueSum),
		MinValueSum:    (*types.BigInt)(p.BallotMode.MinValueSum),
	}
	if err := mode.Validate(); err != nil {
		return nil, fmt.Errorf("invalid ballot mode: %w", err)
	}

	// Validate the census origin
	censusOrigin := types.CensusOrigin(p.Census.CensusOrigin)
	if !censusOrigin.Valid() {
		return nil, fmt.Errorf("invalid census origin: %d", p.Census.CensusOrigin)
	}

	census := types.Census{
		CensusRoot:   p.Census.CensusRoot[:],
		CensusURI:    p.Census.CensusURI,
		CensusOrigin: types.CensusOrigin(p.Census.CensusOrigin),
	}

	results := make([]*types.BigInt, len(p.Result))
	for i, r := range p.Result {
		results[i] = (*types.BigInt)(r)
	}

	return &types.Process{
		Status:         types.ProcessStatus(p.Status),
		OrganizationId: p.OrganizationId,
		EncryptionKey: &types.EncryptionKey{
			X: (*types.BigInt)(p.EncryptionKey.X),
			Y: (*types.BigInt)(p.EncryptionKey.Y),
		},
		StateRoot:             (*types.BigInt)(p.LatestStateRoot),
		StartTime:             time.Unix(int64(p.StartTime.Uint64()), 0),
		Duration:              time.Duration(p.Duration.Uint64()) * time.Second,
		MaxVoters:             (*types.BigInt)(p.MaxVoters),
		VotersCount:           (*types.BigInt)(p.VotersCount),
		OverwrittenVotesCount: (*types.BigInt)(p.OverwrittenVotesCount),
		MetadataURI:           p.MetadataURI,
		BallotMode:            &mode,
		Census:                &census,
		Result:                results,
	}, nil
}

// process2ContractProcess converts a types.Process to a contractProcess
func process2ContractProcess(p *types.Process) contractProcess {
	var prp contractProcess

	prp.Status = uint8(p.Status)
	prp.OrganizationId = p.OrganizationId
	prp.EncryptionKey = npbindings.IProcessRegistryEncryptionKey{
		X: p.EncryptionKey.X.MathBigInt(),
		Y: p.EncryptionKey.Y.MathBigInt(),
	}

	prp.LatestStateRoot = p.StateRoot.MathBigInt()
	prp.StartTime = big.NewInt(p.StartTime.Unix())
	prp.Duration = big.NewInt(int64(p.Duration.Seconds()))
	prp.MaxVoters = p.MaxVoters.MathBigInt()
	prp.MetadataURI = p.MetadataURI

	prp.BallotMode = npbindings.IProcessRegistryBallotMode{
		CostFromWeight: p.BallotMode.CostFromWeight,
		UniqueValues:   p.BallotMode.UniqueValues,
		NumFields:      p.BallotMode.NumFields,
		CostExponent:   p.BallotMode.CostExponent,
		MaxValue:       p.BallotMode.MaxValue.MathBigInt(),
		MinValue:       p.BallotMode.MinValue.MathBigInt(),
		MaxValueSum:    p.BallotMode.MaxValueSum.MathBigInt(),
		MinValueSum:    p.BallotMode.MinValueSum.MathBigInt(),
	}

	copy(prp.Census.CensusRoot[:], p.Census.CensusRoot)
	prp.Census.CensusOrigin = uint8(p.Census.CensusOrigin)
	prp.Census.CensusURI = p.Census.CensusURI
	prp.VotersCount = p.VotersCount.MathBigInt()
	prp.OverwrittenVotesCount = p.OverwrittenVotesCount.MathBigInt()
	if p.Result != nil {
		prp.Result = make([]*big.Int, len(p.Result))
		for i, r := range p.Result {
			prp.Result[i] = r.MathBigInt()
		}
	} else {
		prp.Result = []*big.Int{} // Ensure it's not nil
	}
	return prp
}
