package web3

import (
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethapitypes "github.com/ethereum/go-ethereum/signer/core/apitypes"
	npbindings "github.com/vocdoni/davinci-contracts/golang-types"
	"github.com/vocdoni/davinci-node/spec"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3/rpc"
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
	BlockOverrides *BlockOverrides           `json:"blockOverrides,omitempty"`
	StateOverrides map[string]StateOverride  `json:"stateOverrides,omitempty"`
	Calls          []gethapitypes.SendTxArgs `json:"calls"`
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

// SimulatedBlock is the result of a simulated block
type SimulatedBlock struct {
	Hash   common.Hash  `json:"hash"`
	Number string       `json:"number"`
	Calls  []CallResult `json:"calls"`
}

// CallResult is the result of a single call in a simulated block
type CallResult struct {
	Status     string          `json:"status"` // "0x1" or "0x0"
	ReturnData hexutil.Bytes   `json:"returnData"`
	GasUsed    hexutil.Uint64  `json:"gasUsed"`
	Logs       []gethtypes.Log `json:"logs"`
	Error      *rpc.RPCError   `json:"error,omitempty"`
}

// contractProcess2Process converts a contractProcess to a types.Process
func contractProcess2Process(p npbindings.DAVINCITypesProcess) (*types.Process, error) {
	ballotMode := spec.BallotMode{
		UniqueValues:   p.BallotMode.UniqueValues,
		CostFromWeight: p.BallotMode.CostFromWeight,
		NumFields:      p.BallotMode.NumFields,
		GroupSize:      p.BallotMode.NumFields, // TODO: should be p.BallotMode.GroupSize
		CostExponent:   p.BallotMode.CostExponent,
		MaxValue:       p.BallotMode.MaxValue.Uint64(),
		MinValue:       p.BallotMode.MinValue.Uint64(),
		MaxValueSum:    p.BallotMode.MaxValueSum.Uint64(),
		MinValueSum:    p.BallotMode.MinValueSum.Uint64(),
	}

	// Validate the census origin
	censusOrigin := types.CensusOrigin(p.Census.CensusOrigin)
	if !censusOrigin.Valid() {
		return nil, fmt.Errorf("invalid census origin: %d", p.Census.CensusOrigin)
	}

	census := types.Census{
		CensusRoot:      p.Census.CensusRoot[:],
		CensusURI:       p.Census.CensusURI,
		CensusOrigin:    types.CensusOrigin(p.Census.CensusOrigin),
		ContractAddress: p.Census.ContractAddress,
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
		BallotMode:            ballotMode,
		Census:                &census,
		Result:                results,
	}, nil
}

// process2ContractProcess converts a types.Process to a contractProcess
func process2ContractProcess(p *types.Process) npbindings.DAVINCITypesProcess {
	var prp npbindings.DAVINCITypesProcess

	prp.Status = uint8(p.Status)
	prp.OrganizationId = p.OrganizationId
	prp.EncryptionKey = npbindings.DAVINCITypesEncryptionKey{
		X: p.EncryptionKey.X.MathBigInt(),
		Y: p.EncryptionKey.Y.MathBigInt(),
	}

	prp.LatestStateRoot = p.StateRoot.MathBigInt()
	prp.StartTime = big.NewInt(p.StartTime.Unix())
	prp.Duration = big.NewInt(int64(p.Duration.Seconds()))
	prp.MaxVoters = p.MaxVoters.MathBigInt()
	prp.MetadataURI = p.MetadataURI

	prp.BallotMode = npbindings.DAVINCITypesBallotMode{
		CostFromWeight: p.BallotMode.CostFromWeight,
		UniqueValues:   p.BallotMode.UniqueValues,
		NumFields:      p.BallotMode.NumFields,
		CostExponent:   p.BallotMode.CostExponent,
		MaxValue:       new(big.Int).SetUint64(p.BallotMode.MaxValue),
		MinValue:       new(big.Int).SetUint64(p.BallotMode.MinValue),
		MaxValueSum:    new(big.Int).SetUint64(p.BallotMode.MaxValueSum),
		MinValueSum:    new(big.Int).SetUint64(p.BallotMode.MinValueSum),
	}

	if p.Census.CensusOrigin == types.CensusOriginMerkleTreeOnchainDynamicV1 {
		prp.Census.ContractAddress = p.Census.ContractAddress
	} else {
		copy(prp.Census.CensusRoot[:], p.Census.CensusRoot)
	}
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

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }
